package rabbus

import (
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/rafaeljesus/rabbus"
)

const (
	RABBUS_DSN = "amqp://localhost:5672"
)

var (
	timeout = time.After(3 * time.Second)
)

func TestRabbus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		scenario string
		function func(*testing.T)
	}{
		{
			scenario: "rabbus publish subscribe",
			function: testRabbusPublishSubscribe,
		},
	}

	for _, test := range tests {
		t.Run(test.scenario, func(t *testing.T) {
			test.function(t)
		})
	}
}

func BenchmarkRabbus(b *testing.B) {
	tests := []struct {
		scenario string
		function func(*testing.B)
	}{
		{
			scenario: "rabbus emit async benchmark",
			function: benchmarkEmitAsync,
		},
	}

	for _, test := range tests {
		b.Run(test.scenario, func(b *testing.B) {
			test.function(b)
		})
	}
}

func testRabbusPublishSubscribe(t *testing.T) {
	r, err := rabbus.NewRabbus(rabbus.Config{
		Dsn:     RABBUS_DSN,
		Durable: true,
		Retry: rabbus.Retry{
			Attempts: 1,
		},
		Breaker: rabbus.Breaker{
			Timeout: time.Second * 2,
		},
	})
	if err != nil {
		t.Errorf("Expected to init rabbus %s", err)
	}

	defer func(r rabbus.Rabbus) {
		if err = r.Close(); err != nil {
			t.Errorf("Expected to close rabbus %s", err)
		}
	}(r)

	messages, err := r.Listen(rabbus.ListenConfig{
		Exchange: "test_ex",
		Kind:     "direct",
		Key:      "test_key",
		Queue:    "test_q",
	})
	if err != nil {
		t.Errorf("Expected to listen message %s", err)
	}

	var wg sync.WaitGroup
	wg.Add(1)

	go func(messages chan rabbus.ConsumerMessage) {
		for m := range messages {
			defer wg.Done()
			close(messages)
			m.Ack(false)
		}
	}(messages)

	msg := rabbus.Message{
		Exchange:     "test_ex",
		Kind:         "direct",
		Key:          "test_key",
		Payload:      []byte(`foo`),
		DeliveryMode: rabbus.Persistent,
	}

	r.EmitAsync() <- msg

outer:
	for {
		select {
		case <-r.EmitOk():
			wg.Wait()
			break outer
		case <-r.EmitErr():
			t.Errorf("Expected to emit message")
			break outer
		case <-timeout:
			t.Errorf("parallel.Run() failed, got timeout error")
			break outer
		}
	}
}

func benchmarkEmitAsync(b *testing.B) {
	r, err := rabbus.NewRabbus(rabbus.Config{
		Dsn:     RABBUS_DSN,
		Durable: false,
		Retry: rabbus.Retry{
			Attempts: 1,
		},
		Breaker: rabbus.Breaker{
			Timeout: time.Second * 2,
		},
	})
	if err != nil {
		b.Errorf("Expected to init rabbus %s", err)
	}

	defer func(r rabbus.Rabbus) {
		if err := r.Close(); err != nil {
			b.Errorf("Expected to close rabbus %s", err)
		}
	}(r)

	var wg sync.WaitGroup
	wg.Add(b.N)

	go func(r rabbus.Rabbus) {
		for {
			select {
			case _, ok := <-r.EmitOk():
				if ok {
					wg.Done()
				}
			case _, ok := <-r.EmitErr():
				if ok {
					b.Fatalf("Expected to emit message, receive error: %v", err)
				}
			}
		}
	}(r)

	for n := 0; n < b.N; n++ {
		msg := rabbus.Message{
			Exchange:     "test_bench_ex" + strconv.Itoa(n%10),
			Kind:         "direct",
			Key:          "test_key",
			Payload:      []byte(`foo`),
			DeliveryMode: rabbus.Persistent,
		}

		r.EmitAsync() <- msg
	}

	wg.Wait()
}
