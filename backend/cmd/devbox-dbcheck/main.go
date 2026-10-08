// devbox-dbcheck is copied into a workload and executed there. It reads one
// connection from stdin; passwords never enter argv, image layers or logs.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"time"

	"github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"
)

func main() {
	var input struct {
		Engine, Host, Database, Username, Password string
		Port                                       int
	}
	if err := json.NewDecoder(os.Stdin).Decode(&input); err != nil {
		fmt.Fprintln(os.Stderr, "invalid SQL probe input")
		os.Exit(1)
	}
	driver, dsn := "mysql", ""
	address := net.JoinHostPort(input.Host, strconv.Itoa(input.Port))
	if input.Engine == "postgresql" || input.Engine == "postgres" {
		driver = "postgres"
		u := url.URL{Scheme: "postgres", Host: address, Path: "/" + input.Database, User: url.UserPassword(input.Username, input.Password)}
		q := u.Query()
		q.Set("sslmode", "disable")
		q.Set("connect_timeout", "5")
		u.RawQuery = q.Encode()
		dsn = u.String()
	} else {
		config := mysql.NewConfig()
		config.User, config.Passwd, config.Net, config.Addr, config.DBName = input.Username, input.Password, "tcp", address, input.Database
		config.Timeout, config.ReadTimeout, config.WriteTimeout = 5*time.Second, 5*time.Second, 5*time.Second
		dsn = config.FormatDSN()
	}
	db, err := sql.Open(driver, dsn)
	if err != nil {
		fmt.Fprintln(os.Stderr, "SQL connection configuration failed")
		os.Exit(1)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var result int
	if err := db.QueryRowContext(ctx, "SELECT 1").Scan(&result); err != nil || result != 1 {
		fmt.Fprintln(os.Stderr, "SQL authentication or SELECT 1 failed; check server, database and account grants")
		os.Exit(1)
	}
	fmt.Println("1")
}
