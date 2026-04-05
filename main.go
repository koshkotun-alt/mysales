package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"strconv"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"golang.org/x/crypto/bcrypt"
)

var funcMap = template.FuncMap{
	"json": func(v interface{}) template.JS {
		b, err := json.Marshal(v)
		if err != nil {
			return "" // или можно вернуть some alert/обработку ошибки
		}
		return template.JS(b)
	},
}

var tpl = template.Must(template.New("").Funcs(funcMap).ParseGlob("templates/*.html"))
var db *sql.DB

type User struct {
	ID       int
	Username string
	Password string
}

type Sale struct {
	ID       int
	Product  string
	Quantity int
	Price    float64
	SaleDate time.Time
	UserID   int
}

// сессии храним в памяти (для прототипа)
var sessions = map[string]int{} // session_id: user_id

func main() {
	// Подключение к БД
	dsn := "root:root@tcp(127.0.0.1:3306)/sales_db?parseTime=true"
	var err error
	db, err = sql.Open("mysql", dsn)
	if err != nil {
		panic(err)
	}
	defer db.Close()

	// Маршруты
	http.HandleFunc("/", handleHome)
	http.HandleFunc("/register", handleRegister)
	http.HandleFunc("/login", handleLogin)
	http.HandleFunc("/logout", handleLogout)
	http.HandleFunc("/dashboard", handleDashboard)
	http.HandleFunc("/add_sale", handleAddSale)
	http.HandleFunc("/delete_sale", handleDeleteSale)
	http.HandleFunc("/analytics", handleAnalytics)
	http.HandleFunc("/sales_list", handleSalesList)
	http.HandleFunc("/index", handleIndex)          // добавляем маршрут для главного меню

	fmt.Println("Сайт запущен: http://localhost:8080")
	http.ListenAndServe(":8080", nil)
}

// ----- МИДДЛВАРЫ -----

func getUserIdFromSession(r *http.Request) int {
	c, err := r.Cookie("session_id")
	if err != nil { return 0 }
	if uid, ok := sessions[c.Value]; ok { return uid }
	return 0
}

// ----- Хэндлеры -----

func handleHome(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func handleRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		tpl.ExecuteTemplate(w, "register.html", nil)
		return
	}
	username := r.FormValue("username")
	password := r.FormValue("password")
	if username == "" || password == "" {
		tpl.ExecuteTemplate(w, "register.html", "Поля обязательны")
		return
	}
	// Хэшируем пароль
	hash, _ := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	_, err := db.Exec("INSERT INTO users (username, password) VALUES (?, ?)", username, hash)
	if err != nil {
		tpl.ExecuteTemplate(w, "register.html", "Имя занято")
		return
	}
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		tpl.ExecuteTemplate(w, "login.html", nil)
		return
	}
	username := r.FormValue("username")
	password := r.FormValue("password")
	var user User
	err := db.QueryRow("SELECT id, password FROM users WHERE username=?", username).Scan(&user.ID, &user.Password)
	if err != nil {
		tpl.ExecuteTemplate(w, "login.html", "Неверный логин или пароль")
		return
	}
	err = bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(password))
	if err != nil {
		tpl.ExecuteTemplate(w, "login.html", "Неверный логин или пароль")
		return
	}
	// создаем простую сессию (безопасно для прототипа)
	session := fmt.Sprintf("%d_%d", user.ID, time.Now().UnixNano())
	sessions[session] = user.ID
	http.SetCookie(w, &http.Cookie{
		Name:  "session_id",
		Value: session,
		Path:  "/",
	})
	// редирект на главный экран
	http.Redirect(w, r, "/index", http.StatusSeeOther)
}

func clearSession(w http.ResponseWriter, r *http.Request) {
	c, err := r.Cookie("session_id")
	if err != nil { return }
	// удаление сессии из карты
	delete(sessions, c.Value)
	// удаление куки
	cookie := &http.Cookie{
		Name:   "session_id",
		Value:  "",
		Path:   "/",
		MaxAge: -1,
	}
	http.SetCookie(w, cookie)
}

func handleLogout(w http.ResponseWriter, r *http.Request) {
	clearSession(w, r)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func handleIndex(w http.ResponseWriter, r *http.Request) {
	// Проверка, что пользователь авторизован
	uid := getUserIdFromSession(r)
	if uid == 0 {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	tpl.ExecuteTemplate(w, "main_menu.html", nil)
}

func handleDashboard(w http.ResponseWriter, r *http.Request) {
	uid := getUserIdFromSession(r)
	if uid == 0 {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	rows, err := db.Query("SELECT id, product, quantity, price, sale_date FROM sales WHERE user_id=?", uid)
	if err != nil {
		http.Error(w, "Ошибка получения данных", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var sales []Sale
	for rows.Next() {
		var s Sale
		err := rows.Scan(&s.ID, &s.Product, &s.Quantity, &s.Price, &s.SaleDate)
		if err != nil {
			continue
		}
		sales = append(sales, s)
	}
	tpl.ExecuteTemplate(w, "dashboard.html", map[string]interface{}{
		"Sales": sales,
	})
}

func handleAddSale(w http.ResponseWriter, r *http.Request) {
	uid := getUserIdFromSession(r)
	if uid == 0 {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	if r.Method == "GET" {
		tpl.ExecuteTemplate(w, "add_sale.html", nil)
		return
	}
	product := r.FormValue("product")
	quantity, _ := strconv.Atoi(r.FormValue("quantity"))
	price, _ := strconv.ParseFloat(r.FormValue("price"), 64)
	if product == "" || quantity <= 0 || price <= 0 {
		tpl.ExecuteTemplate(w, "add_sale.html", "Некорректные данные")
		return
	}
	_, err := db.Exec("INSERT INTO sales (product, quantity, price, user_id) VALUES (?, ?, ?, ?)",
		product, quantity, price, uid)
	if err != nil {
		tpl.ExecuteTemplate(w, "add_sale.html", "Ошибка сохранения")
		return
	}
	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

func handleDeleteSale(w http.ResponseWriter, r *http.Request) {
	uid := getUserIdFromSession(r)
	if uid == 0 {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	saleId, _ := strconv.Atoi(r.URL.Query().Get("id"))
	db.Exec("DELETE FROM sales WHERE id=? AND user_id=?", saleId, uid)
	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

func handleSalesList(w http.ResponseWriter, r *http.Request) {
	uid := getUserIdFromSession(r)
	if uid == 0 {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	rows, _ := db.Query("SELECT id, product, quantity, price, sale_date FROM sales WHERE user_id=? ORDER BY sale_date DESC", uid)
	sales := []Sale{}
	for rows.Next() {
		var s Sale
		rows.Scan(&s.ID, &s.Product, &s.Quantity, &s.Price, &s.SaleDate)
		sales = append(sales, s)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(sales)
}

func handleAnalytics(w http.ResponseWriter, r *http.Request) {
	uid := getUserIdFromSession(r)
	if uid == 0 {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	// Получаем даты
	now := time.Now()
	currentYear, currentMonth, _ := now.Date()

	// Месяц и год прошлого месяца
	lastMonth := now.AddDate(0, -1, 0)
	lastYear, lastMonthNumber, _ := lastMonth.Date()

	// Формируем диапазоны дат
	startCurrentMonth := time.Date(currentYear, currentMonth, 1, 0, 0, 0, 0, time.Local)
	startLastMonth := time.Date(lastYear, lastMonthNumber, 1, 0, 0, 0, 0, time.Local)
	endCurrentMonth := startCurrentMonth.AddDate(0, 1, 0)
	endLastMonth := startLastMonth.AddDate(0, 1, 0)

	// ---------------------------
	// Получаем суммы продаж за текущий месяц, прошлый месяц, за весь год
	var sumCurrent, sumLast, sumYear float64

	sqlSum := "SELECT SUM(quantity * price) FROM sales WHERE user_id=? AND sale_date >= ? AND sale_date < ?"
	// текущий месяц
	row := db.QueryRow(sqlSum, uid, startCurrentMonth, endCurrentMonth)
	row.Scan(&sumCurrent)

	// прошлый месяц
	row = db.QueryRow(sqlSum, uid, startLastMonth, endLastMonth)
	row.Scan(&sumLast)

	// весь год — с 1 января текущего года по сегодняшний день
	startYear := time.Date(currentYear, 1, 1, 0, 0, 0, 0, time.Local)
	row = db.QueryRow(sqlSum, uid, startYear, now)
	row.Scan(&sumYear)

	// ---------------------------
	// Получаем данные по месяцам для графика за последний год
	// создадим массив с 12 месяцами
	type MonthData struct {
		Month string
		Sum   float64
	}
	var monthsData []MonthData
	for i := 0; i < 12; i++ {
		monthStart := time.Date(currentYear, time.Month(i+1), 1, 0, 0, 0, 0, time.Local)
		monthEnd := monthStart.AddDate(0, 1, 0)
		var monthSum float64
		row := db.QueryRow("SELECT SUM(quantity * price) FROM sales WHERE user_id=? AND sale_date >= ? AND sale_date < ?", uid, monthStart, monthEnd)
		row.Scan(&monthSum)
		monthsData = append(monthsData, MonthData{
			Month: monthStart.Format("Jan"),
			Sum:   monthSum,
		})
	}

	// подготовка данных для Chart.js
	var labels []string
	var data []float64
	for _, md := range monthsData {
		labels = append(labels, md.Month)
		data = append(data, md.Sum)
	}

	tpl.ExecuteTemplate(w, "analytics.html", map[string]interface{}{
		"SumCurrent": sumCurrent,
		"SumLast":    sumLast,
		"SumYear":    sumYear,
		"Labels":     labels,
		"Data":       data,
	})
}
