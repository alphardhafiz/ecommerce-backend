package repository

// ProductError carries the offending product's identity for PRD C.8
// ("error jelas menyebutkan item mana yang bermasalah").
type ProductError struct {
	Code        string
	ProductID   string
	ProductName string
}

func (e *ProductError) Error() string { return e.Code + ": " + e.ProductName }
