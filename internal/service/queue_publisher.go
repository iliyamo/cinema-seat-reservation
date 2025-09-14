// Package queue_publisher provides functions to publish domain events to RabbitMQ.
// Errors are logged and returned to allow callers to ignore failures without
// interrupting the main request flow.
package queue_publisher

import (
    "context"
    "encoding/json"
    "log"
    "os"
    "time"

    amqp "github.com/rabbitmq/amqp091-go"

    q "github.com/iliyamo/cinema-seat-reservation/internal/queue"
)

// PublishBookingConfirmed publishes a BookingConfirmedEvent to the
// "booking.confirmed" queue. The function attempts to be robust and
// to never panic; any error is logged and returned so the caller can
// choose to ignore it. Messages are marked as persistent.
func PublishBookingConfirmed(ctx context.Context, event q.BookingConfirmedEvent) error {
    url := os.Getenv("RABBITMQ_URL")
    if url == "" {
        url = os.Getenv("AMQP_URL")
    }
    if url == "" {
        url = "amqp://guest:guest@localhost:5672/"
    }
    conn, err := amqp.Dial(url)
    if err != nil {
        log.Printf("rabbitmq: dial failed: %v", err)
        return err
    }
    defer func() { _ = conn.Close() }()

    ch, err := conn.Channel()
    if err != nil {
        log.Printf("rabbitmq: channel open failed: %v", err)
        return err
    }
    defer func() { _ = ch.Close() }()

    // Ensure the queue exists (idempotent). Durable so messages survive broker restarts.
    if _, err := ch.QueueDeclare(
        "booking.confirmed", // name
        true,                 // durable
        false,                // autoDelete
        false,                // exclusive
        false,                // noWait
        nil,                  // args
    ); err != nil {
        log.Printf("rabbitmq: queue declare failed: %v", err)
        return err
    }

    body, err := json.Marshal(event)
    if err != nil {
        log.Printf("rabbitmq: marshal event failed: %v", err)
        return err
    }

    pub := amqp.Publishing{
        ContentType:  "application/json",
        DeliveryMode: amqp.Persistent, // store on disk
        Timestamp:    time.Now().UTC(),
        Body:         body,
    }

    if err := ch.PublishWithContext(ctx,
        "",                 // default exchange
        "booking.confirmed", // routing key = queue name
        false,               // mandatory
        false,               // immediate
        pub,
    ); err != nil {
        log.Printf("rabbitmq: publish failed: %v", err)
        return err
    }

    return nil
}