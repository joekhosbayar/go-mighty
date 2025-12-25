package main

import (
	"fmt"
	"strings"
)

func main() {
	// Strings, Runes, Stringbuilders

	myString := "rèsumé"
	indexed := myString[1]
	fmt.Println(myString)
	fmt.Println(indexed)
	fmt.Printf("Value: %v, Type: %T\n", indexed, indexed)

	fmt.Println("Bytes in string:")
	for i := 0; i < len(myString); i++ {
		b := myString[i]
		fmt.Printf("Index: %d, Byte: %x\n", i, b)
	}

	fmt.Println("Runes in string:")
	for i, v := range myString {
		fmt.Printf("Index: %d, Value: %c\n", i, v)
	}

	// Do not use string concatenation in loops because strings are immutable and it creates many copies

	// use strings.Builder for efficient string concatenation in loops
	myBuilder := strings.Builder{}

	strSlice := []string{"Go", "is", "awesome!"}
	for i := range strSlice {
		myBuilder.WriteString(strSlice[i])
	}
	finalString := myBuilder.String()
	fmt.Println(finalString)
}
