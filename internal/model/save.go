package model

import (
	"time"

	"github.com/google/uuid"
)

type Save struct {
	ID           uuid.UUID `json:"id"`
	UserID       uuid.UUID `json:"user_id"`
	Name         string    `json:"name"`
	CreatedAt    time.Time `json:"created_at"`
	FactoryCount int       `json:"factory_count"`
	ModCount     int       `json:"mod_count"`
}

type Factory struct {
	ID      uuid.UUID `json:"id"`
	SaveID  uuid.UUID `json:"save_id"`
	Name    string    `json:"name"`
	Src     bool      `json:"src"`
	PLCount int       `json:"pl_count"`
}

type PLGroup struct {
	ID        uuid.UUID `json:"id"`
	FactoryID uuid.UUID `json:"factory_id"`
	Name      string    `json:"name"`
	Position  string    `json:"position"`
}

type DependentProductionLine struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	FactoryID   uuid.UUID `json:"factory_id"`
	FactoryName string    `json:"factory_name"`
}

type FactorySource struct {
	ID        uuid.UUID `json:"id"`
	FactoryID uuid.UUID `json:"factory_id"`
	ModID     string    `json:"mod_id"`
	ItemID    string    `json:"item_id"`
	Name      string    `json:"name"`
	RateNum   int       `json:"rate_num"`
	RateDen   int       `json:"rate_den"`
	TimeUnit  string    `json:"time_unit"`
}
