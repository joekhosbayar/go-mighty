package main

import (
	"fmt"
	"unicode/utf8"
)

func main() {
	fmt.Println("Hello, World!")
	var intNum int16 = 32767 // int16 can hold values from -32768 to 32767
	intNum += 1              // This will cause an overflow, but compile without error
	fmt.Println("intNum:", intNum)

	var floatNum float32 = 3.402823466e+38 // float32 can hold very large values
	floatNum *= 2
	fmt.Println("floatNum:", floatNum)

	var result float32 = floatNum + float32(intNum) // not allowed to add different types directly
	fmt.Println("result:", result)

	var integerDivision int = 5 / 3 // integer division

	var myString string = "The value of integer division is rounded down: " + fmt.Sprint(integerDivision)
	fmt.Println(myString)

	// len function counts the number of bytes, not characters. UTF8 encoding uses more than one byte for some characters.
	var myUTF8String string = "世界"
	fmt.Println("Length of myUTF8String in bytes:", len(myUTF8String))

	// To properly count characters, use the UTF8 rune slice conversion
	fmt.Println("Number of characters in myUTF8String using RuneCountInString:", utf8.RuneCountInString(myUTF8String))

	var myRune rune = 'a'
	fmt.Println("myRune:", myRune)

	const pi float32 = 3.1415926535
	fmt.Println("Constant pi:", pi)
}
