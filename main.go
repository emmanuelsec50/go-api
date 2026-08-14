package main

import (
	"context"
	"fmt"

	"github.com/emmanuelsec50/chi-router/application"
)

func main() {
	app := application.New()
	err := app.Start(context.TODO())
	if err != nil {
		fmt.Println("Failed to start app:", err)
	}
}
