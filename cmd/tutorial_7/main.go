package main

import "fmt"

func main() {
	var p *int32 = new(int32)
	var i int32 = 42
	fmt.Println(*p)
	p = &i
	fmt.Println(*p)
	foo(p)
	fmt.Println(*p)

	foo(&i)
	fmt.Println(i)

	thing1 := [5]float32{1.0, 2.0, 3.0}

	fmt.Printf("value: %v, address: %p\n", thing1, &thing1)

	changeThing(&thing1)

	fmt.Printf("value: %v, address: %p\n", thing1, &thing1)
}

func foo(changeMe *int32) {
	fmt.Println(*changeMe)
	*changeMe = 21
}

func changeThing(thing *[5]float32) {
	fmt.Printf("value: %v, address: %p\n", *thing, thing)
	(*thing)[0] = 42.0
}
