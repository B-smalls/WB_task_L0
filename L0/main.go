package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"

	"errors"

	"github.com/nats-io/stan.go"

	"database/sql"

	_ "github.com/lib/pq"
)

type Delivery struct {
	Name    string `json:"name"`
	Phone   string `json:"phone"`
	Zip     string `json:"zip"`
	City    string `json:"city"`
	Address string `json:"address"`
	Region  string `json:"region"`
	Email   string `json:"email"`
}

type Payment struct {
	Transaction  string `json:"transaction"`
	RequestID    string `json:"request_id"`
	Currency     string `json:"currency"`
	Provider     string `json:"provider"`
	Amount       int    `json:"amount"`
	PaymentDT    int64  `json:"payment_dt"`
	Bank         string `json:"bank"`
	DeliveryCost int    `json:"delivery_cost"`
	GoodsTotal   int    `json:"goods_total"`
	CustomFee    int    `json:"custom_fee"`
}

type Item struct {
	ChrtID      int    `json:"chrt_id"`
	TrackNumber string `json:"track_number"`
	Price       int    `json:"price"`
	RID         string `json:"rid"`
	Name        string `json:"name"`
	Sale        int    `json:"sale"`
	Size        string `json:"size"`
	TotalPrice  int    `json:"total_price"`
	NmID        int    `json:"nm_id"`
	Brand       string `json:"brand"`
	Status      int    `json:"status"`
}

type Order struct {
	OrderUID        string   `json:"order_uid"`
	TrackNumber     string   `json:"track_number"`
	Entry           string   `json:"entry"`
	Delivery        Delivery `json:"delivery"`
	Payment         Payment  `json:"payment"`
	Items           []Item   `json:"items"`
	Locale          string   `json:"locale"`
	InternalSig     string   `json:"internal_signature"`
	CustomerID      string   `json:"customer_id"`
	DeliveryService string   `json:"delivery_service"`
	ShardKey        string   `json:"shardkey"`
	SMID            int      `json:"sm_id"`
	DateCreated     string   `json:"date_created"`
	OOFShard        string   `json:"oof_shard"`
}

// Функция для валидации заказа
func validateOrder(order Order) error {
	// Проверка обязательных полей
	if order.OrderUID == "" {
		return errors.New("OrderUID field is required")
	}
	if order.TrackNumber == "" {
		return errors.New("TrackNumber field is required")
	}
	if order.Delivery.Name == "" {
		return errors.New("Delivery Name field is required")
	}
	if order.Delivery.Phone == "" {
		return errors.New("Delivery Phone field is required")
	}
	if order.Delivery.Zip == "" {
		return errors.New("Delivery Zip field is required")
	}
	return nil
}

// Обработчик для страницы с формой ввода
func indexHandler(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "index.html")
}

var cache map[string]Order

func main() {

	// Обработчик для страницы с формой ввода
	http.HandleFunc("/", indexHandler)

	// Инициализация кэша
	cache = make(map[string]Order)

	// Создаем URL для подключения к базе данных
	dbURL := &url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword("postgres", "password"),
		Host:     "localhost",
		Path:     "test_db",
		RawQuery: "sslmode=disable",
	}

	// Преобразуем URL в строку и открываем соединение с базой данных
	db, err := sql.Open("postgres", dbURL.String())
	if err != nil {
		log.Fatalf("Error: Unable to connect to database: %v", err)
	}
	defer db.Close()

	// Теперь у вас есть открытое соединение с базой данных
	fmt.Println("Successfully connected to the database!")

	// Подключение к Nats-Streaming
	sc, err := stan.Connect("test-cluster", "client-id")
	if err != nil {
		log.Fatal(err)
	}
	defer sc.Close()

	// Подписка на канал в Nats-Streaming
	sub, err := sc.Subscribe("orders_1", func(msg *stan.Msg) {
		var order Order
		err := json.Unmarshal(msg.Data, &order)
		if err != nil {
			log.Println("Error decoding message:", err)
			return
		}

		// Проверяем валидность заказа перед сохранением
		if err := validateOrder(order); err != nil {
			log.Println("Invalid order:", err)
			return
		}

		// Сохранение данных в базе данных PostgreSQL
		if err := saveOrderToDB(db, order); err != nil {
			log.Println("Error saving order to database:", err)
			return
		}

		// Обновление кэша
		cache[order.OrderUID] = order
	}, stan.DurableName("orders-service"))
	if err != nil {
		log.Fatal(err)
	}
	defer sub.Close()

	// HTTP обработчик для получения данных о заказе
	http.HandleFunc("/order", func(w http.ResponseWriter, r *http.Request) {
		orderUID := r.URL.Query().Get("order_uid")
		if orderUID == "" {
			http.Error(w, "Missing order_uid parameter", http.StatusBadRequest)
			return
		}

		// Проверка наличия данных о заказе в кэше
		if order, ok := cache[orderUID]; ok {
			json.NewEncoder(w).Encode(order)
			return
		}

		// Если данных нет в кэше, попытаемся получить из базы данных
		order, err := getFromDB(db, orderUID)
		if err != nil {
			http.Error(w, "Order not found", http.StatusNotFound)
			return
		}
		// Обновление кэша
		cache[orderUID] = order

		json.NewEncoder(w).Encode(order)
	})

	// Запуск веб-сервера на порту 8080
	serverErr := http.ListenAndServe(":8080", nil)
	if serverErr != nil {
		fmt.Println("Ошибка запуска сервера:", serverErr)
	}
}

// Функция для сохранения данных в бд
func saveOrderToDB(db *sql.DB, order Order) error {
	// Проверяем, существует ли заказ с таким же OrderUID
	existingOrder, err := getFromDB(db, order.OrderUID)
	if err != nil && err != sql.ErrNoRows {
		return fmt.Errorf("error checking if order exists in database: %v", err)
	}
	if existingOrder.OrderUID != "" {
		// Заказ уже существует, обработайте эту ситуацию по вашему усмотрению
		// Например, можно просто вывести сообщение об ошибке
		return fmt.Errorf("order with ID %s already exists in the database", order.OrderUID)
	}

	// Преобразование структуры Order в JSON
	orderJSON, err := json.Marshal(order)
	if err != nil {
		return fmt.Errorf("error marshaling order to JSON: %v", err)
	}

	// Выполнение SQL-запроса для вставки данных в таблицу
	_, err = db.Exec(`INSERT INTO orders (order_uid, "order") VALUES ($1, $2)`, order.OrderUID, orderJSON)
	if err != nil {
		return fmt.Errorf("error inserting order into database: %v", err)
	}

	return nil
}

// Функция для получения данных из бд
func getFromDB(db *sql.DB, orderUID string) (Order, error) {
	var order Order
	var orderJSON string // Переменная для хранения JSON

	// Запрос данных из базы данных
	err := db.QueryRow(`SELECT "order" FROM orders WHERE order_uid = $1`, orderUID).Scan(&orderJSON)
	if err != nil {
		return Order{}, err
	}

	// Десериализация JSON в структуру Order
	if err := json.Unmarshal([]byte(orderJSON), &order); err != nil {
		return Order{}, fmt.Errorf("error unmarshaling JSON: %v", err)
	}

	return order, nil
}
