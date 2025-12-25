package main

import (
	"errors"
	"fmt"
)

func main() {
	var numerator = 10
	var denominator = 20
	quotient, remainder, err := integerDivision(numerator, denominator)

	// if / else
	if err != nil {
		fmt.Println("Error:", err.Error())
	} else if remainder == 0 {
		fmt.Printf("The quotient is %d and there is no remainder\n", quotient)
	} else {
		fmt.Printf("The quotient is %d and the remainder is %d\n", quotient, remainder)
	}

	// switch
	switch {
	case remainder == 0:
		fmt.Printf("The quotient is %d and there is no remainder\n", quotient)
	default:
		fmt.Printf("The quotient is %d and the remainder is %d\n", quotient, remainder)
	}

	// conditional switch
	switch remainder {
	case 0:
		fmt.Printf("The quotient is %d and there is no remainder\n", quotient)
	default:
		fmt.Printf("The quotient is %d and the remainder is %d\n", quotient, remainder)
	}

}

// this is a general design pattern in GO. Functions return multiple values, and handle errors like this.
func integerDivision(a int, b int) (int, int, error) {
	var err error
	if b == 0 {
		err = errors.New("division by zero is not allowed")
		return 0, 0, err
	}
	quotient := a / b
	remainder := a % b
	return quotient, remainder, err
}
