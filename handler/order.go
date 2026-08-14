package handler

import (
	"fmt"
	"net/http"
)

type Order struct{}

func (o *Order) Create(w http.ResponseWriter, r *http.Request) {
	fmt.Println("Create an order")
}
func (o *Order) List(w http.ResponseWriter, r *http.Request) {
	fmt.Println("List all orders")
}
func (o *Order) GetbyID(w http.ResponseWriter, r *http.Request) {
	fmt.Println("Get an order by ID")
}
func (o *Order) UpdateByID(w http.ResponseWriter, r *http.Request) {
	fmt.Println("Updated an order by ID")
}
func (o *Order) DeleteByID(w http.ResponseWriter, r *http.Request) {
	fmt.Println("Deleted an oder by ID")
}
