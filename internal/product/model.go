package product

import "time"

type Product struct {
	ID        int64
	Name      string
	Price     float64
	Stock     int
	CreatedAt time.Time
}
