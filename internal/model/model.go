package model

import (
	"time"

	"github.com/google/uuid"
)

type User struct {
	ID           uuid.UUID
	Username     string
	Email        string
	PasswordHash string
	CreatedAt    time.Time
}

type Token struct {
	ID        uuid.UUID
	UserID    uuid.UUID
	TokenHash string
	Type      string // "session" | "refresh"
	ExpiresAt time.Time
}

type Save struct {
	ID        uuid.UUID
	UserID    uuid.UUID
	Name      string
	CreatedAt time.Time
}

type Factory struct {
	ID     uuid.UUID
	SaveID uuid.UUID
	Name   string
}

type ProductionLine struct {
	ID           uuid.UUID
	FactoryID    *uuid.UUID // nil bei Draft-PLs
	ParentPLID   *uuid.UUID // nil for root PLs
	Name         string
	TargetModID  string
	TargetItemID string
	RateNum      int
	RateDen      int
	TimeUnit     string // "t" | "s" | "min" | "h"
	OptimizeMode string // "TARGET" | "AUTO"
	Status       string // "draft" | "active" | "archived"
}

type MachineGroup struct {
	ID            uuid.UUID
	PLID          uuid.UUID
	MachineModID  string
	MachineID     string
	RecipeID      uuid.UUID
	Count         int
	UpgradeTierID *uuid.UUID
	UpgradeCount  int
	Status        string // "draft" | "planned" | "built" | "archived"
}

type PLIO struct {
	ID          uuid.UUID
	PLID        uuid.UUID
	Direction   string // "input" | "output"
	IOType      string // "item" | "fluid"
	ModID       string
	ItemFluidID string
	RateNum     int
	RateDen     int
	IsStopPoint bool
}

type SolverDraft struct {
	ID        uuid.UUID
	FactoryID uuid.UUID
	UserID    uuid.UUID
	Result    []byte // JSONB
	ExpiresAt time.Time
	CreatedAt time.Time
}
