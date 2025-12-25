package main

import (
	"fmt"
)

func main() {
	arrays()
	maps()
	loops()
}

func loops() {
	// for loop
	for i := 0; i < 5; i++ {
		fmt.Println("For loop iteration:", i)
	}

	// while loop (Go doesn't have while, but we can use for to mimic it)
	j := 0
	for j < 5 {
		fmt.Println("While loop iteration:", j)
		j++
	}

	for i, v := range []string{"apple", "banana", "cherry"} {
		fmt.Printf("Index: %d, Value: %s\n", i, v)
	}

}

func maps() {
	myMap := make(map[string]uint8)
	fmt.Println("map1 Initial map:", myMap)

	myMap2 := map[string]uint8{
		"Alice": 30,
		"Bob":   25,
	}
	fmt.Println("map2 Map with initial values:", myMap2)

	// Maps always have a value even if the key was never set. Maps return a secondary boolean value to indicate if the key was found.
	name, ok := myMap2["Joe"]
	if ok {
		fmt.Println("Found Joe's age:", name)
	} else {
		fmt.Println("Joe not found")
	}

}

func arrays() {
	var intArr [3]int32
	fmt.Println(intArr)
	intArr[0] = 42
	intArr[1] = 27
	intArr[2] = 99
	fmt.Println(intArr)

	intArr2 := [3]int32{7, 14, 21}
	fmt.Println(intArr2)

	intArr3 := [...]int32{3, 6, 8, 12}
	fmt.Println(intArr3)

	var slice = []int32{5, 10, 15, 20, 25}
	fmt.Println(slice)
	fmt.Printf("Length of slice: %v with capacity: %v\n", len(slice), cap(slice))

	slice = append(slice, 30)
	fmt.Println("After appending 30:", slice)
	fmt.Printf("Length of slice: %v with capacity: %v\n", len(slice), cap(slice))

	// append with spread operator
	slice2 := []int32{40, 50, 60}
	fmt.Println("Slice 2 is :", slice2)

	slice = append(slice, slice2...)
	fmt.Println("After appending slice2 to slice1: ", slice)
	fmt.Printf("Length of slice is: %v with capacity: %v\n", len(slice), cap(slice))

	// make function
	slice3 := make([]int32, 5, 100)
	fmt.Println("Slice 3 is :", slice3)
	fmt.Printf("Length of slice3 is: %v with capacity: %v\n", len(slice3), cap(slice3))
}
