package main

import "fmt"

type engine interface {
	milesLeft() uint8
}

type gasEngine struct {
	mpg     uint8
	gallons uint8
	owner   owner
}

type electricEngine struct {
	kwh    uint8
	charge uint8
	owner  owner
}

type owner struct {
	name string
	age  uint8
}

func (e gasEngine) milesLeft() uint8 {
	return e.mpg * e.gallons
}

func (e electricEngine) milesLeft() uint8 {
	return e.kwh * e.charge / 100
}

func canMakeIt(engine engine, miles uint8) {
	if engine.milesLeft() >= miles {
		fmt.Println("You can make it!")
	} else {
		fmt.Println("You cannot make it.")
	}
}

func main() {
	// struct fields are set in order
	myGasEngine1 := gasEngine{10, 20, owner{"Charlie", 40}}
	fmt.Println("My gas engine 1, fields are set in order:", myGasEngine1)

	// if fields not set, they take default zero values
	myGasEngine2 := gasEngine{}
	fmt.Println("My gas engine 2, fields are default values:", myGasEngine2)

	// can call setters individually
	myGasEngine2.mpg = 15
	myGasEngine2.gallons = 10
	myGasEngine2.owner = owner{"Diana", 35}
	fmt.Println("My gas engine 2, fields are set individually:", myGasEngine2)

	// struct literal
	myGasEngine := gasEngine{
		mpg:     25,
		gallons: 15,
		owner: owner{
			name: "Alice",
			age:  30,
		},
	}
	fmt.Println("My gas engine:", myGasEngine)

	myElectricEngine := electricEngine{
		kwh:    200,
		charge: 50,
		owner: owner{
			name: "Bob",
			age:  25,
		},
	}
	fmt.Println("My electric engine:", myElectricEngine)

	canMakeIt(myGasEngine, 255)
	canMakeIt(myElectricEngine, 128)
}
