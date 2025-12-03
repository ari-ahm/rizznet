package model

import (
	"time"
)

type Proxy struct {
	ID        uint   `gorm:"primaryKey"`
	Hash      string `gorm:"uniqueIndex"`
	Raw       string
	Source    string
	CreatedAt time.Time

	// Connection Details (Entry Point)
	Address string // The IP or Domain we connect to
	Port    int

	// Entry Metadata (Inbound - The server we connect to)
	EntryIP      string
	EntryISP     string
	EntryCountry string

	// Exit Metadata (Outbound - What the world sees)
	Country    string
	ISP        string
	IP         string
	IsDirty    bool
	IsRotating bool

	// Relationships
	Performances []ProxyPerformance `gorm:"foreignKey:ProxyID"`
	Categories   []Category         `gorm:"many2many:proxy_categories;"`
}

type ProxyPerformance struct {
	ProxyID uint   `gorm:"primaryKey;autoIncrement:false"` // Composite Key Part 1
	UserISP string `gorm:"primaryKey;autoIncrement:false"` // Composite Key Part 2 (The environment ISP)

	Score       float64 // Normalized Moving Average (0.0 - 1.0+)
	SampleCount int     // How many times we tested this
	LastTested  time.Time
}

type Category struct {
	ID      uint    `gorm:"primaryKey"`
	Name    string  `gorm:"uniqueIndex"`
	Proxies []Proxy `gorm:"many2many:proxy_categories;"`
}
