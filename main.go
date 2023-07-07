package main

import (
	"context"
	"fmt"
	"github.com/gempir/go-twitch-irc/v4"
	amqp "github.com/rabbitmq/amqp091-go"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"os"
	"strconv"
	"time"
)

func main() {

	// Load environment variables
	botUsername := os.Getenv("TWITCH_BOT_USERNAME")
	botToken := os.Getenv("TWITCH_BOT_TOKEN")
	twitchChannel := os.Getenv("TWITCH_BOT_CHANNEL")
	welcomeMessage := os.Getenv("TWITCH_BOT_WELCOME_MESSAGE")

	rabbitmqUsername := os.Getenv("RABBITMQ_USERNAME")
	rabbitmqPassword := os.Getenv("RABBITMQ_PASSWORD")
	rabbitmqHost := os.Getenv("RABBITMQ_HOST")
	rabbitmqPort, err := strconv.Atoi(os.Getenv("RABBITMQ_PORT"))
	if err != nil {
		fmt.Println("Error during RabbitMQ Port environment variable conversion ")
		return
	}

	// Init Zap Logger
	loggerMgr := initZapLog()
	zap.ReplaceGlobals(loggerMgr)
	defer loggerMgr.Sync()
	logger := loggerMgr.Sugar()

	// Init RabbitMQ Queues
	ctx, ch := initQueues(logger, rabbitmqUsername, rabbitmqPassword, rabbitmqHost, rabbitmqPort)

	// Init Twitch Client
	client := initTwitchClient(logger, ctx, ch, botUsername, botToken, twitchChannel, welcomeMessage)
	err = client.Connect()
	if err != nil {
		panic(err)
	}
}

func initZapLog() *zap.Logger {
	config := zap.NewProductionConfig()
	config.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	config.EncoderConfig.TimeKey = "timestamp"
	config.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	logger, _ := config.Build()

	return logger
}

func initQueues(logger *zap.SugaredLogger, rabbitmqUsername string,
	rabbitmqPassword string, rabbitmqHost string, rabbitmqPort int) (context.Context, *amqp.Channel) {

	conn, err := amqp.Dial(fmt.Sprintf("amqp://%s:%s@%s:%d",
		rabbitmqUsername, rabbitmqPassword, rabbitmqHost, rabbitmqPort))
	if err != nil {
		logger.Panicf("%s: %s", "Failed to connect to RabbitMQ", err)
	}
	defer conn.Close()

	ch, err := conn.Channel()
	if err != nil {
		logger.Panicf("%s: %s", "Failed to open a channel", err)
	}
	defer ch.Close()

	err = ch.ExchangeDeclare(
		"private-message",
		"fanout",
		true,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		logger.Panicf("%s: %s", "Failed to declare a private-message exchange", err)
	}

	err = ch.ExchangeDeclare(
		"whisper-message",
		"fanout",
		true,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		logger.Panicf("%s: %s", "Failed to declare a whisper-message exchange", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	return ctx, ch
}

func initTwitchClient(logger *zap.SugaredLogger, context context.Context,
	channel *amqp.Channel, botUsername string, botToken string,
	twitchChannel string, welcomeMessage string) *twitch.Client {

	client := twitch.NewClient(botUsername, botToken)

	client.OnConnect(func() {
		logger.Info(fmt.Sprintf("Conectado com o nome de usuário %s ao canal %s",
			botUsername, twitchChannel))
		client.Say(twitchChannel, welcomeMessage)
	})

	client.OnPrivateMessage(func(message twitch.PrivateMessage) {
		logger.Info(fmt.Sprintf("Mensagem recebida de %s: %s", message.User.DisplayName, message.Message))
		err := channel.PublishWithContext(context,
			"private-message",
			"",
			false,
			false,
			amqp.Publishing{
				ContentType: "text/plain",
				Body:        []byte(message.Message),
			})
		if err != nil {
			logger.Panicf("%s: %s", "Failed to publish message", err)
		}
	})

	client.OnWhisperMessage(func(message twitch.WhisperMessage) {
		logger.Info(fmt.Sprintf("Mensagem recebida de %s: %s", message.User.DisplayName, message.Message))
		err := channel.PublishWithContext(context,
			"whisper-message",
			"",
			false,
			false,
			amqp.Publishing{
				ContentType: "text/plain",
				Body:        []byte(message.Message),
			})
		if err != nil {
			logger.Panicf("%s: %s", "Failed to publish message", err)
		}
	})

	client.Join(twitchChannel)

	return client
}
