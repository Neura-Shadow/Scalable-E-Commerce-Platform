package money

import (
	"errors"
	"math"
)

const MinorUnitScale int64 = 100

var ErrInvalidAmount = errors.New("invalid monetary amount")

func ToMinorUnits(amount float64) (int64, error) {
	if math.IsNaN(amount) || math.IsInf(amount, 0) || amount < 0 {
		return 0, ErrInvalidAmount
	}

	scaled := amount * float64(MinorUnitScale)
	if scaled >= float64(math.MaxInt64) {
		return 0, ErrInvalidAmount
	}
	rounded := math.Round(scaled)
	if math.Abs(scaled-rounded) > 1e-7 {
		return 0, ErrInvalidAmount
	}
	return int64(rounded), nil
}

func FromMinorUnits(amount int64) float64 {
	return float64(amount) / float64(MinorUnitScale)
}

func Multiply(unitPrice int64, quantity uint) (int64, error) {
	if unitPrice < 0 || uint64(quantity) > math.MaxInt64 {
		return 0, ErrInvalidAmount
	}
	quantity64 := int64(quantity)
	if quantity64 != 0 && unitPrice > math.MaxInt64/quantity64 {
		return 0, ErrInvalidAmount
	}
	return unitPrice * quantity64, nil
}

func Add(left, right int64) (int64, error) {
	if left < 0 || right < 0 || left > math.MaxInt64-right {
		return 0, ErrInvalidAmount
	}
	return left + right, nil
}
