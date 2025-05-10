package core

import "math/rand"

const alphanumeric = "abcdefghijklmnopqrstuvwxyz0123456789"

func GenerateRandomID() string {
	bytes := make([]byte, 5)
	for i := range bytes {
		bytes[i] = alphanumeric[rand.Intn(len(alphanumeric))]
	}
	return string(bytes)
}
