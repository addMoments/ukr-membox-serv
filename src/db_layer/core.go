package db

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/huandu/go-sqlbuilder"
	"github.com/lib/pq"
	_ "github.com/lib/pq"
)

type DatabaseConfig struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	Password string `json:"password"`
	DBName   string `json:"dbname"`
}

func (c *DatabaseConfig) ToConnString() string {
	return fmt.Sprintf("host=%s port=%d user=%s "+
		"password=%s dbname=%s sslmode=disable binary_parameters=yes",
		c.Host, c.Port, c.Username, c.Password, c.DBName)
}

func (c *DatabaseConfig) ListenForNotifications(ntfName string, callback func(data string)) {
	listener := pq.NewListener(c.ToConnString(), 10*time.Second, time.Minute, func(ev pq.ListenerEventType, err error) {
		if err != nil {
			fmt.Println("listener error:", err)
		}
	})

	err := listener.Listen(ntfName)
	if err != nil {
		panic(err)
	}

	for {
		select {
		case n := <-listener.Notify:
			if n != nil {
				callback(n.Extra)
			}
		case <-time.After(90 * time.Second):
			// ping to keep connection alive
			listener.Ping()
		}
	}
}

var is_db_init = false
var db *sql.DB

func Db() *sql.DB {
	return db
}

func Init(c DatabaseConfig) {
	loc_db, err := sql.Open("postgres", c.ToConnString())
	if err != nil {
		fmt.Println(err)
		panic(err.Error() + "1")
	}

	res, err := loc_db.Query("SELECT 'abc';")
	if err != nil {
		fmt.Println(err)
		panic(err.Error() + "2")
	}
	defer res.Close()

	byt, err := scan_one_int(res, 1)
	if err != nil {
		fmt.Println(err)
		panic(err.Error() + "3")
	}

	if string(byt[0]) != "abc" {
		panic("connection not verified, expected 'abc' got '" + string(byt[0]) + "'")
	}

	fmt.Println("database ping successful")

	db = loc_db
	is_db_init = true

}

func Query_one(b sqlbuilder.Builder) ([][]byte, error) {
	if !is_db_init {
		return nil, fmt.Errorf("db is not init")
	}

	sql_str, args := b.BuildWithFlavor(sqlbuilder.PostgreSQL)

	//fmt.Println(sql_str, args)

	r, err := db.Query(sql_str, args...)
	if err != nil {
		return nil, err
	}
	defer r.Close()

	cols, err := r.Columns()
	if err != nil {
		return nil, err
	}

	if len(cols) == 0 {
		return make([][]byte, 0), nil
	}

	return scan_one_int(r, len(cols))
}

func Exec(b sqlbuilder.Builder) error {
	if !is_db_init {
		return fmt.Errorf("db is not init")
	}

	sql_str, args := b.BuildWithFlavor(sqlbuilder.PostgreSQL)

	//fmt.Println(sql_str, args)

	_, err := db.Exec(sql_str, args...)
	return err
}

func Query_all(b sqlbuilder.Builder) ([][][]byte, error) {
	if !is_db_init {
		return nil, fmt.Errorf("db is not init")
	}

	sql_str, args := b.BuildWithFlavor(sqlbuilder.PostgreSQL)

	//fmt.Println(sql_str, args, "query_all")

	r, err := db.Query(sql_str, args...)
	if err != nil {
		return nil, err
	}

	return scan_all(r)
}

func scan_all(r *sql.Rows) ([][][]byte, error) {
	res := make([][][]byte, 0)

	cols, err := r.Columns()
	if err != nil {
		return nil, err
	}

	for {
		next_bytes, err := scan_one_int(r, len(cols))
		if err != nil {
			break
		}
		res = append(res, next_bytes)
	}

	r.Close()

	return res, nil
}

func scan_one_int(r *sql.Rows, col_len int) ([][]byte, error) {
	if !r.Next() {
		return nil, fmt.Errorf("empty row")
	}

	res := make([][]byte, col_len)
	address_ar := make([]interface{}, col_len)

	for i := 0; i < col_len; i++ {
		res[i] = make([]byte, 0)
		address_ar[i] = &res[i]
	}

	err := r.Scan(address_ar...)

	return res, err
}
