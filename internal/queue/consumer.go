// Package queue contains the background consumer that listens to the
// booking.confirmed queue and writes structured logs to logs/booking.log.
package queue

import (
    "encoding/json"
    "errors"
    "fmt"
    "log"
    "os"
    "path/filepath"
    "strings"
    "time"

    amqp "github.com/rabbitmq/amqp091-go"
)

const bookingQueueName = "booking.confirmed"

// StartBookingConsumer connects to RabbitMQ, declares the booking.confirmed
// queue (durable), and starts consuming messages. Each message is appended to
// logs/booking.log in a single-line, human-friendly format. The function
// runs a reconnect loop and only returns an error if the initial context is
// cancelled; otherwise it keeps running and logs any processing errors while
// rejecting the offending message so the server continues operating.
func StartBookingConsumer() error {
    url := os.Getenv("RABBITMQ_URL")
    if url == "" {
        url = os.Getenv("AMQP_URL")
    }
    if url == "" {
        url = "amqp://guest:guest@localhost:5672/"
    }

    backoff := time.Second
    for {
        conn, err := amqp.Dial(url)
        if err != nil {
            log.Printf("booking-consumer: failed to dial broker: %v; retrying in %s", err, backoff)
            time.Sleep(backoff)
            if backoff < 30*time.Second {
                backoff *= 2
            }
            continue
        }
        backoff = time.Second // reset after successful connect

        if err := consumeLoop(conn); err != nil {
            log.Printf("booking-consumer: consume loop ended: %v; reconnecting", err)
            // Sleep briefly before reconnect
            time.Sleep(2 * time.Second)
            continue
        }
    }
}

func consumeLoop(conn *amqp.Connection) error {
    ch, err := conn.Channel()
    if err != nil {
        return fmt.Errorf("channel open: %w", err)
    }
    defer func() { _ = ch.Close() }()

    if err := ch.Qos(50, 0, false); err != nil {
        log.Printf("booking-consumer: set QoS failed: %v", err)
    }

    _, err = ch.QueueDeclare(bookingQueueName, true, false, false, false, nil)
    if err != nil {
        return fmt.Errorf("queue declare: %w", err)
    }

    msgs, err := ch.Consume(bookingQueueName, "", false, false, false, false, nil)
    if err != nil {
        return fmt.Errorf("queue consume: %w", err)
    }

    for d := range msgs {
        if err := handleMessage(d.Body); err != nil {
            log.Printf("booking-consumer: handle message failed: %v", err)
            _ = d.Nack(false, false) // reject, do not requeue to avoid tight loops
            continue
        }
        _ = d.Ack(false)
    }
    return errors.New("deliveries channel closed")
}

func handleMessage(body []byte) error {
    var ev BookingConfirmedEvent
    if err := json.Unmarshal(body, &ev); err != nil {
        return fmt.Errorf("unmarshal: %w", err)
    }
    // Ensure logs directory exists
    if err := os.MkdirAll("logs", 0o755); err != nil {
        return fmt.Errorf("mkdir logs: %w", err)
    }
    fpath := filepath.Join("logs", "booking.log")
    f, err := os.OpenFile(fpath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
    if err != nil {
        return fmt.Errorf("open log file: %w", err)
    }
    defer f.Close()

    seats := "[]"
    if len(ev.SeatLabels) > 0 {
        seats = fmt.Sprintf("[%s]", strings.Join(ev.SeatLabels, ","))
    }

    line := fmt.Sprintf("[%s] Reservation confirmed | reservation_id=%d | user_id=%d | show_id=%d | cinema=\"%s\" | hall=\"%s\" | movie=\"%s\" | total=%d cents | seats=%s\n",
        ev.ConfirmedAt, ev.ReservationID, ev.UserID, ev.ShowID, ev.CinemaName, ev.HallName, ev.MovieTitle, ev.TotalAmountCents, seats)

    if _, err := f.WriteString(line); err != nil {
        return fmt.Errorf("write log: %w", err)
    }
    return nil
}