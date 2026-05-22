package promo

import (
	"errors"
	"fmt"
	"math"
	db "membox-serv/src/db_layer"
	"strconv"
	"strings"

	"github.com/huandu/go-sqlbuilder"
)

const premiumProductID = "premium"

type ErrorCode string

const (
	ErrCodeRequired                ErrorCode = "promo_code_required"
	ErrNotFound                    ErrorCode = "promo_not_found"
	ErrInactive                    ErrorCode = "promo_inactive"
	ErrNotStarted                  ErrorCode = "promo_not_started"
	ErrExpired                     ErrorCode = "promo_expired"
	ErrUsageLimitReached           ErrorCode = "promo_usage_limit_reached"
	ErrUnsupportedDiscountType     ErrorCode = "promo_unsupported_discount_type"
	ErrRequiresPremium             ErrorCode = "promo_requires_premium"
	ErrInvalidPurchaseInfo         ErrorCode = "invalid_purchase_info"
	ErrInvalidPromoDiscountValue   ErrorCode = "promo_invalid_discount_value"
	ErrInvalidProductPriceSnapshot ErrorCode = "invalid_product_price"
)

type ValidationError struct {
	Code    ErrorCode
	Message string
}

func (e ValidationError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return string(e.Code)
}

func NewValidationError(code ErrorCode, message string) ValidationError {
	return ValidationError{Code: code, Message: message}
}

func ErrorCodeOf(err error) (ErrorCode, bool) {
	var validationErr ValidationError
	if errors.As(err, &validationErr) {
		return validationErr.Code, true
	}
	return "", false
}

type Quote struct {
	PromoCodeUID          string  `json:"promo_code_uid,omitempty"`
	PromoCodeTextSnapshot string  `json:"promo_code_text_snapshot,omitempty"`
	GrossTotal            float64 `json:"gross_total"`
	DiscountAmount        float64 `json:"discount_amount"`
	NetTotal              float64 `json:"net_total"`
}

type promoRow struct {
	UID             string
	Code            string
	DiscountType    string
	DiscountValue   float64
	IsActive        bool
	HasStarted      bool
	NotExpired      bool
	UsageLimitOpen  bool
	DeletedAtIsNull bool
}

type productLine struct {
	ID       string
	Price    float64
	Quantity int
}

// NormalizeCode keeps promo comparisons consistent with the DB unique index:
// users may type different casing or accidental surrounding spaces.
func NormalizeCode(code string) string {
	return strings.ToUpper(strings.TrimSpace(code))
}

// Validate calculates the promo quote without writing anything to the DB.
// Purchase creation and checkout preview both use this so the premium-only
// discount rule stays identical across both flows.
func Validate(code string, purchaseInfo map[string]int) (Quote, error) {
	normalizedCode := NormalizeCode(code)
	if normalizedCode == "" {
		return Quote{}, NewValidationError(ErrCodeRequired, "promo code is required")
	}

	lines, err := loadProductLines(purchaseInfo)
	if err != nil {
		return Quote{}, err
	}

	grossTotal, premiumSubtotal := calculateSubtotals(lines)
	if premiumSubtotal <= 0 {
		return Quote{}, NewValidationError(ErrRequiresPremium, "promo code requires premium product")
	}

	promo, err := loadPromo(normalizedCode)
	if err != nil {
		return Quote{}, err
	}
	if err := validatePromoState(promo); err != nil {
		return Quote{}, err
	}

	discountAmount := roundMoney(premiumSubtotal * promo.DiscountValue / 100)
	if discountAmount < 0 || discountAmount > grossTotal {
		return Quote{}, NewValidationError(ErrInvalidPromoDiscountValue, "invalid promo discount amount")
	}

	return Quote{
		PromoCodeUID:          promo.UID,
		PromoCodeTextSnapshot: normalizedCode,
		GrossTotal:            roundMoney(grossTotal),
		DiscountAmount:        discountAmount,
		NetTotal:              roundMoney(grossTotal - discountAmount),
	}, nil
}

// loadPromo reads the promo row and asks Postgres to evaluate date/limit flags
// against the DB clock, avoiding app-server clock drift for validity checks.
func loadPromo(normalizedCode string) (promoRow, error) {
	sb := sqlbuilder.NewSelectBuilder()
	sb.Select(
		"uid",
		"code",
		"discount_type",
		"discount_value",
		"is_active",
		"(valid_from <= NOW()) AS has_started",
		"(valid_until IS NULL OR valid_until >= NOW()) AS not_expired",
		"(usage_limit_total IS NULL OR usage_count < usage_limit_total) AS usage_limit_open",
		"(deleted_at IS NULL) AS deleted_at_is_null",
	).From("promo_codes").Where(
		sb.Equal("UPPER(BTRIM(code))", normalizedCode),
	)

	row, err := db.Query_one(sb)
	if err != nil {
		return promoRow{}, NewValidationError(ErrNotFound, "promo code not found")
	}

	discountValue, err := strconv.ParseFloat(string(row[3]), 64)
	if err != nil {
		return promoRow{}, err
	}

	return promoRow{
		UID:             string(row[0]),
		Code:            NormalizeCode(string(row[1])),
		DiscountType:    string(row[2]),
		DiscountValue:   discountValue,
		IsActive:        parsePgBool(row[4]),
		HasStarted:      parsePgBool(row[5]),
		NotExpired:      parsePgBool(row[6]),
		UsageLimitOpen:  parsePgBool(row[7]),
		DeletedAtIsNull: parsePgBool(row[8]),
	}, nil
}

// validatePromoState separates promo failure reasons so the apply endpoint can
// return stable error codes to the frontend.
func validatePromoState(promo promoRow) error {
	if !promo.DeletedAtIsNull {
		return NewValidationError(ErrNotFound, "promo code not found")
	}
	if !promo.IsActive {
		return NewValidationError(ErrInactive, "promo code is inactive")
	}
	if promo.DiscountType != "percent" {
		return NewValidationError(ErrUnsupportedDiscountType, "promo discount type is not supported")
	}
	if promo.DiscountValue <= 0 || promo.DiscountValue > 100 {
		return NewValidationError(ErrInvalidPromoDiscountValue, "invalid promo discount value")
	}
	if !promo.HasStarted {
		return NewValidationError(ErrNotStarted, "promo code is not active yet")
	}
	if !promo.NotExpired {
		return NewValidationError(ErrExpired, "promo code has expired")
	}
	if !promo.UsageLimitOpen {
		return NewValidationError(ErrUsageLimitReached, "promo code usage limit has been reached")
	}
	return nil
}

// loadProductLines mirrors the current purchase request contract: purchaseInfo
// is keyed by products.id, not product uid. Unknown or non-positive items are
// rejected before any quote is calculated.
func loadProductLines(purchaseInfo map[string]int) ([]productLine, error) {
	if len(purchaseInfo) == 0 {
		return nil, NewValidationError(ErrInvalidPurchaseInfo, "purchase info is empty")
	}

	productIDs := make([]string, 0, len(purchaseInfo))
	for productID, quantity := range purchaseInfo {
		productID = strings.TrimSpace(productID)
		if productID == "" || quantity <= 0 {
			return nil, NewValidationError(ErrInvalidPurchaseInfo, "purchase info contains invalid item")
		}
		productIDs = append(productIDs, productID)
	}

	sb := sqlbuilder.NewSelectBuilder()
	sb.Select("id", "price").
		From("products").
		Where(sb.In("id", db.Interface_ar(productIDs)...))

	rows, err := db.Query_all(sb)
	if err != nil {
		return nil, err
	}
	if len(rows) != len(productIDs) {
		return nil, NewValidationError(ErrInvalidPurchaseInfo, "purchase info contains unknown product")
	}

	lines := make([]productLine, 0, len(rows))
	for _, row := range rows {
		price, err := strconv.ParseFloat(string(row[1]), 64)
		if err != nil {
			return nil, NewValidationError(ErrInvalidProductPriceSnapshot, "invalid product price")
		}
		productID := string(row[0])
		lines = append(lines, productLine{
			ID:       productID,
			Price:    price,
			Quantity: purchaseInfo[productID],
		})
	}

	return lines, nil
}

// calculateSubtotals keeps add-ons in the gross total but excludes them from
// the discount base. This is the central promo business rule.
func calculateSubtotals(lines []productLine) (grossTotal float64, premiumSubtotal float64) {
	for _, line := range lines {
		lineTotal := line.Price * float64(line.Quantity)
		grossTotal += lineTotal
		if line.ID == premiumProductID {
			premiumSubtotal += lineTotal
		}
	}
	return roundMoney(grossTotal), roundMoney(premiumSubtotal)
}

func parsePgBool(value []byte) bool {
	switch strings.ToLower(string(value)) {
	case "t", "true", "1":
		return true
	default:
		return false
	}
}

func roundMoney(value float64) float64 {
	return math.Round(value*100) / 100
}

func (q Quote) String() string {
	return fmt.Sprintf(
		"gross=%.2f discount=%.2f net=%.2f promo=%s",
		q.GrossTotal,
		q.DiscountAmount,
		q.NetTotal,
		q.PromoCodeTextSnapshot,
	)
}
