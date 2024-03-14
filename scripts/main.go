package main

import (
	"log"
	"os"

	"github.com/nats-io/stan.go"
)

func main() {
	// Подключение к NATS Streaming
	sc, err := stan.Connect("test-cluster", "client-id_1")
	if err != nil {
		log.Fatal(err)
	}
	defer sc.Close()

	// Открытие файла с сообщениями
	file, err := os.Open("messages.json")
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()

	// Получение содержимого файла
	stat, err := file.Stat()
	if err != nil {
		log.Fatal(err)
	}

	data := make([]byte, stat.Size())
	_, err = file.Read(data)
	if err != nil {
		log.Fatal(err)
	}

	// Отправка сообщения в NATS Streaming
	err = sc.Publish("orders_1", data)
	if err != nil {
		log.Fatal(err)
	}

	log.Println("Message sent successfully")
}
