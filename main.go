package main

import (
	"bytes"
	"context"
	"encoding/gob"
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
	conn, ctx, ch := initQueues(logger, rabbitmqUsername, rabbitmqPassword, rabbitmqHost, rabbitmqPort)
	defer conn.Close()
	defer ch.Close()

	// Init Twitch Client
	client := initTwitchClient(logger, ctx, ch, botUsername, botToken, twitchChannel, welcomeMessage)
	client.Join(twitchChannel)
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

func initQueues(logger *zap.SugaredLogger, rabbitmqUsername string, rabbitmqPassword string, rabbitmqHost string, rabbitmqPort int) (*amqp.Connection, context.Context, *amqp.Channel) {

	conn, err := amqp.Dial(fmt.Sprintf("amqp://%s:%s@%s:%d",
		rabbitmqUsername, rabbitmqPassword, rabbitmqHost, rabbitmqPort))
	if err != nil {
		logger.Panicf("%s: %s", "Failed to connect to RabbitMQ", err)
	}

	ch, err := conn.Channel()
	if err != nil {
		logger.Panicf("%s: %s", "Failed to open a channel", err)
	}

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
		logger.Panicf("%s: %s", "Failed to declare the private-message exchange", err)
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
		logger.Panicf("%s: %s", "Failed to declare the whisper-message exchange", err)
	}

	_, err = ch.QueueDeclare(
		"send-twitch-chat",
		false,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		logger.Panicf("%s: %s", "Failed to declare the send-twitch-chat", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	return conn, ctx, ch
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

		buffer := new(bytes.Buffer)
		enc := gob.NewEncoder(buffer)

		buffer.Reset()
		err := enc.Encode(message)
		if err != nil {
			logger.Panicf("%s: %s", "Failed to encode received message", err)
		}

		logger.Info(fmt.Sprintf("Mensagem recebida de %s: %s", message.User.DisplayName, message.Message))
		err = channel.PublishWithContext(context,
			"private-message",
			"",
			false,
			false,
			amqp.Publishing{
				ContentType: "text/plain",
				Body:        buffer.Bytes(),
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

	sendTwitchChatQueueInit(logger, channel)

	return client
}

func sendTwitchChatQueueInit(logger *zap.SugaredLogger, channel *amqp.Channel) {
	msgs, err := channel.Consume(
		"send-twitch-chat",
		"",
		true,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		logger.Panicf("%s: %s", "Failed to request a consumer", err)
	}

	go func() {
		for d := range msgs {
			fmt.Println(string(d.Body))
		}
	}()
}
