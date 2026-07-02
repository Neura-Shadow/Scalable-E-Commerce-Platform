package dbs

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSafeOrderByColumnAllowsKnownColumn(t *testing.T) {
	order := SafeOrderByColumn("price", true, "created_at", map[string]string{
		"price": "price",
	})

	assert.Equal(t, "price", order.Column.Name)
	assert.True(t, order.Desc)
}

func TestSafeOrderByColumnFallsBackForUnknownColumn(t *testing.T) {
	order := SafeOrderByColumn("price DESC; DROP TABLE users;--", true, "created_at", map[string]string{
		"price": "price",
	})

	assert.Equal(t, "created_at", order.Column.Name)
	assert.False(t, order.Desc)
}
