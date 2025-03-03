package event

import (
	"update_shipping_details/config/kafka"
	message "update_shipping_details/internal/data/messages"
	dbcontext "update_shipping_details/internal/repository"
	"update_shipping_details/internal/secrets"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/IBM/sarama"
)

// StartKafkaConsumer initializes and runs the Kafka consumer in a separate goroutine
func StartKafkaConsumer(serviceID string) {
	topic := "shipping_details"
	msgCnt := 0

	// Initialize Kafka consumer
	consumer, err := GetKafkaConsumer(secrets.GetKafkaBrokers())
	if err != nil {
		log.Fatalf("Error obtaining Kafka consumer: %v", err)
	}
	worker, err := consumer.ConsumePartition(topic, 0, sarama.OffsetOldest)
	if err != nil {
		panic(err)
	}
	defer worker.Close()

	isServiceActive := false
	fmt.Println("Kafka consumer started")

	replicator := dbcontext.NewReplicator(*dbcontext.NewShippingDetailsRepository(dbcontext.GetDBClient().Database("shipping_details")))

	// Handle OS signals for graceful shutdown
	sigchan := make(chan os.Signal, 1)
	signal.Notify(sigchan, syscall.SIGINT, syscall.SIGTERM)

	for {
		select {
		case err := <-worker.Errors():
			fmt.Println("Kafka Error:", err)
		case msg := <-worker.Messages():
			msgCnt++
			fmt.Printf("Received message %d: | Topic(%s)\n", msgCnt, msg.Topic)

			var ShippingDetailsMsg message.ShippingDetailsTopicMessage
			err := json.Unmarshal(msg.Value, &ShippingDetailsMsg)
			if err != nil {
				fmt.Println("Error parsing Kafka message:", err)
				continue
			}

		
			// Log parsed message
			fmt.Printf("Parsed Kafka Message: %+v\n", ShippingDetailsMsg)
			if ShippingDetailsMsg.Target == serviceID {
				fmt.Printf("Message targeted to this service: %+v\n", ShippingDetailsMsg)
				// Handle database updates
				if ShippingDetailsMsg.Action == "db-update" {
					fmt.Println("Database update detected, syncing offset...")
					kafka.GetOffsetManager().SetOffset(msg.Offset)
					isServiceActive = true
					fmt.Println("Service is now active")
				}
			} else if ShippingDetailsMsg.Target == "all" && ShippingDetailsMsg.Service != serviceID {
				if isServiceActive {
					fmt.Printf("Processing update for ShippingDetails ID: %s\n", ShippingDetailsMsg.ShippingDetails.ID)
					replicator.Replicate(ShippingDetailsMsg.ShippingDetails, ShippingDetailsMsg.Action)
				}
			}

		case <-sigchan:
			fmt.Println("Interrupt detected, shutting down consumer...")
			return
		}
	}
}
