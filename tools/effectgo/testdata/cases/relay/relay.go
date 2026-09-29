package relay

import "cases/party"

// Greeter hands out a function over party.Name, so a caller holds a value
// whose type names a package it does not import.
func Greeter() func(party.Name) string {
	return func(name party.Name) string { return "hello " + string(name) }
}

// Ada is a name, for a caller that cannot spell the type.
func Ada() party.Name { return "ada" }
