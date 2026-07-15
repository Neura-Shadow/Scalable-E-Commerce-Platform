package model

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type Cart struct {
	ID        string      `json:"id" gorm:"primaryKey"`
	CreatedAt time.Time   `json:"created_at"`
	UpdatedAt time.Time   `json:"updated_at"`
	DeletedAt *time.Time  `json:"deleted_at" gorm:"index"`
	UserID    string      `json:"user_id" gorm:"not null;uniqueIndex:idx_cart_user"`
	User      *User       `gorm:"foreignKey:UserID;references:ID"`
	Lines     []*CartLine `json:"lines" gorm:"foreignKey:CartID;references:ID"`
}

type CartLine struct {
	CartID    string   `json:"cart_id" gorm:"primaryKey"`
	ProductID string   `json:"product_id" gorm:"primaryKey"`
	Product   *Product `gorm:"foreignKey:ProductID;references:ID"`
	Quantity  uint     `json:"quantity"`
}

func (cart *Cart) BeforeCreate(tx *gorm.DB) error {
	cart.ID = uuid.New().String()
	return nil
}
