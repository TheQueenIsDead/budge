package main

import "gorm.io/gorm"

type BudgetFrequency string

const (
	Weekly  BudgetFrequency = "w"
	Monthly BudgetFrequency = "m"
	Yearly  BudgetFrequency = "y"
)

type BudgetItem struct {
	gorm.Model

	Name      string          `goorm:"name"`
	Cost      float64         `goorm:"cost"`
	Frequency BudgetFrequency `goorm:"frequency"`
}
