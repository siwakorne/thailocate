package main

import (
	"encoding/json"
	"fmt"
	"log"

	"github.com/siwakorne/thailocate"
)

func main() {
	loc, err := thailocate.New()
	if err != nil {
		log.Fatal(err)
	}

	points := [][2]float64{
		{13.7563, 100.5018}, // Bangkok - near Siam
		{18.7883, 98.9853},  // Chiang Mai
		{7.8804, 98.3923},   // Phuket
		{0, 0},              // out in the ocean, not in Thailand
	}

	for _, p := range points {
		lat, lng := p[0], p[1]
		detail, err := loc.GetLocationDetail(lat, lng)
		if err != nil {
			log.Fatal(err)
		}
		b, _ := json.MarshalIndent(detail, "", "  ")
		fmt.Println(string(b))
	}
}
