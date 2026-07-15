package money

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMinorUnitConversionAndArithmetic(t *testing.T) {
	unitPrice, err := ToMinorUnits(0.10)
	require.NoError(t, err)
	lineTotal, err := Multiply(unitPrice, 3)
	require.NoError(t, err)
	total, err := Add(lineTotal, 20)
	require.NoError(t, err)

	assert.Equal(t, int64(50), total)
	assert.Equal(t, 0.50, FromMinorUnits(total))
}

func TestMinorUnitsRejectsUnsupportedPrecisionAndOverflow(t *testing.T) {
	_, err := ToMinorUnits(1.001)
	assert.ErrorIs(t, err, ErrInvalidAmount)
	_, err = ToMinorUnits(math.Inf(1))
	assert.ErrorIs(t, err, ErrInvalidAmount)
	_, err = ToMinorUnits(float64(math.MaxInt64) / float64(MinorUnitScale))
	assert.ErrorIs(t, err, ErrInvalidAmount)
	_, err = Multiply(math.MaxInt64, 2)
	assert.ErrorIs(t, err, ErrInvalidAmount)
	_, err = Add(math.MaxInt64, 1)
	assert.ErrorIs(t, err, ErrInvalidAmount)
}
