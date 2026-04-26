package main

import (
	"fmt"
	"time"

	"github.com/miekg/dns"
)

func main() {
	c := &dns.Client{Timeout: 10 * time.Second}
	domain := "example.co.uk."
	m := new(dns.Msg)
	m.SetQuestion(domain, dns.TypeSOA)

	r, _, err := c.Exchange(m, "1.1.1.1:53")
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	fmt.Printf("Answer count: %d\n", len(r.Answer))
	for _, a := range r.Answer {
		fmt.Printf("Answer: %v\n", a)
	}
	fmt.Printf("Ns count: %d\n", len(r.Ns))
	for _, a := range r.Ns {
		fmt.Printf("Ns: %v\n", a)
	}
}
