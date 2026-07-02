package repository

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestInventoryListOrderUsesAllowedColumn(t *testing.T) {
	order := inventoryListOrder("quantity", true)

	assert.Equal(t, "quantity", order.Column.Name)
	assert.True(t, order.Desc)
}

func TestInventoryListOrderRejectsUnsafeColumn(t *testing.T) {
	order := inventoryListOrder("quantity DESC; DROP TABLE inventories;--", true)

	assert.Equal(t, "created_at", order.Column.Name)
	assert.False(t, order.Desc)
}
