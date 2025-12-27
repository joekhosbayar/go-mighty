package main

import (
	"encoding/json"
	"fmt"
	"io"
)

func main() {
	fmt.Println("Generics")
	fmt.Println("Adding floats")
	res := add(1.0, 2.0)
	fmt.Println(res)

	var contacts []contactInfo = loadJSON[contactInfo]("contacts.json")
	fmt.Println(contacts)

	var purchases []purchaseInfo = loadJSON[purchaseInfo]("purchases.json")
	fmt.Println(purchases)

	var gasCar = car[gasEngine]{
		carMake:  "Honda",
		carModel: "Civic",
		engine: gasEngine{
			mpg:     20.0,
			gallons: 10.0,
		},
	}
	fmt.Println(gasCar)

}

func add[T float32 | float64 | int32 | int64](x T, y T) T {
	return x + y
}

type contactInfo struct {
	Name  string
	Email string
}

type purchaseInfo struct {
	Name   string
	Price  float32
	Amount int
}

// In this scenario, the object type from the file cannot be inferred by the compiler.
// Therefore in the main method, we must specify if this is a contact or purchase info
func loadJSON[T contactInfo | purchaseInfo](filePath string) []T {
	data, _ := io.Readfile(filePath)
	var loaded = []T{}
	err := json.Unmarshal(data, &loaded)
	if err != nil {
		return nil
	}
	return loaded
}

// using generics with structs
type gasEngine struct {
	gallons float32
	mpg     float32
}

type electricEngine struct {
	kwh    float32
	mphkwh float32
}

type car[T gasEngine | electricEngine] struct {
	carMake  string
	carModel string
	engine   T
}
