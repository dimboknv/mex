package copytrading

// IsOpenOrder возвращает true если side означает открытие позиции
func IsOpenOrder(side int) bool {
	return side == 1 || side == 3
}

// GetSideText возвращает текстовое описание side
func GetSideText(side int) string {
	switch side {
	case 1:
		return "LONG"
	case 2:
		return "SHORT"
	case 3:
		return "SHORT"
	case 4:
		return "LONG"
	default:
		return "UNKNOWN"
	}
}
