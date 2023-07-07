package main

import (
	"context"
	"fmt"
	"github.com/gempir/go-twitch-irc/v4"
	amqp "github.com/rabbitmq/amqp091-go"
	"go.uber.org/zap"
	"os"
	"time"
)

func main() {

	logger, _ := zap.NewProduction()
	defer logger.Sync()
	sugar := logger.Sugar()

	conn, err := amqp.Dial("amqp://user:password@localhost:5672")
	if err != nil {
		sugar.Panicf("%s: %s", "Failed to connect to RabbitMQ", err)
	}
	defer conn.Close()

	ch, err := conn.Channel()
	if err != nil {
		sugar.Panicf("%s: %s", "Failed to open a channel", err)
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
		sugar.Panicf("%s: %s", "Failed to declare a exchange", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	username := os.Getenv("TWITCH_BOT_USERNAME")
	token := os.Getenv("TWITCH_BOT_TOKEN")
	channel := os.Getenv("TWITCH_BOT_CHANNEL")

	client := twitch.NewClient(username, token)

	client.OnConnect(func() {
		sugar.Info(fmt.Sprintf("Conectado com o nome de usuário %s ao canal %s", username, channel))
		client.Say(channel, "A mãe tá on! HeyGuys")
	})

	client.OnPrivateMessage(func(message twitch.PrivateMessage) {
		sugar.Info(fmt.Sprintf("Mensagem recebida de %s: %s", message.User.DisplayName, message.Message))
		err := ch.PublishWithContext(ctx,
			"private-message",
			"",
			false,
			false,
			amqp.Publishing{
				ContentType: "text/plain",
				Body:        []byte(message.Message),
			})
		if err != nil {
			sugar.Panicf("%s: %s", "Failed to publish message", err)
		}
	})

	client.OnWhisperMessage(func(message twitch.WhisperMessage) {
		sugar.Info(fmt.Sprintf("Sussurro recebido de %s: %s", message.User.DisplayName, message.Message))
	})

	client.Join(channel)

	err = client.Connect()
	if err != nil {
		panic(err)
	}
}
