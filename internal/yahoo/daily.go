package yahoo

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"time"
)

type StockMeta struct {
	StockCode  string
	StockName  string
	Region     string
	SectorID   int64
	SectorName sql.NullString
}

type VolumeStock struct {
	StockCode  string
	StockName  string
	SectorID   int64
	SectorName sql.NullString
	ClosePrice float64
	MaxPrice   float64
	MinPrice   float64
	Rise       float64
	Amount     float64
	Vol        float64
	GmtCreate  time.Time
}

type ShadowStock struct {
	StockCode  string
	StockName  string
	SectorID   int64
	SectorName sql.NullString
	ClosePrice float64
	MaxPrice   float64
	MinPrice   float64
	Rise       float64
	Amount     float64
	Vol        float64
	GmtCreate  time.Time
}

type BreakoutStock struct {
	StockCode      string
	StockName      string
	SectorID       int64
	SectorName     sql.NullString
	ClosePrice     float64
	MaxPrice       float64
	MinPrice       float64
	Rise           float64
	Amount         float64
	Vol            float64
	BeforeMaxPrice float64
	BeforeMaxVol   float64
	BeforeMaxTime  time.Time
	GmtCreate      time.Time
}

const minPoolAmount = 500000000

type chartResponse struct {
	Chart struct {
		Result []struct {
			Timestamp []int64 `json:"timestamp"`
			Meta      struct {
				Symbol              string  `json:"symbol"`
				RegularMarketPrice  float64 `json:"regularMarketPrice"`
				PreviousClose       float64 `json:"previousClose"`
				ChartPreviousClose  float64 `json:"chartPreviousClose"`
				RegularMarketVolume int64   `json:"regularMarketVolume"`
			} `json:"meta"`
			Indicators struct {
				Quote []struct {
					Close  []*float64 `json:"close"`
					High   []*float64 `json:"high"`
					Low    []*float64 `json:"low"`
					Open   []*float64 `json:"open"`
					Volume []*int64   `json:"volume"`
				} `json:"quote"`
			} `json:"indicators"`
		} `json:"result"`
		Error any `json:"error"`
	} `json:"chart"`
}

func LoadFirstYahooSupportedStock(ctx context.Context, db *sql.DB) (StockMeta, error) {
	stocks, err := LoadYahooSupportedStocks(ctx, db)
	if err != nil {
		return StockMeta{}, fmt.Errorf("load first yahoo supported stock failed: %w", err)
	}
	if len(stocks) == 0 {
		return StockMeta{}, fmt.Errorf("no yahoo supported stock found")
	}
	return stocks[0], nil
}

func LoadYahooSupportedStocks(ctx context.Context, db *sql.DB) ([]StockMeta, error) {
	rows, err := db.QueryContext(
		ctx,
		`
SELECT stock_code, stock_name, region, sector_id, sector_name
FROM stock
WHERE region IN ('SH', 'SZ')
ORDER BY id
`,
	)
	if err != nil {
		return nil, fmt.Errorf("load yahoo supported stocks failed: %w", err)
	}
	defer rows.Close()

	var stocks []StockMeta
	for rows.Next() {
		var meta StockMeta
		if err := rows.Scan(&meta.StockCode, &meta.StockName, &meta.Region, &meta.SectorID, &meta.SectorName); err != nil {
			return nil, fmt.Errorf("scan stock meta failed: %w", err)
		}
		if isSTStockName(meta.StockName) {
			continue
		}
		stocks = append(stocks, meta)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate stock meta failed: %w", err)
	}
	return stocks, nil
}

func YahooSymbol(meta StockMeta) (string, error) {
	switch meta.Region {
	case "SH":
		return meta.StockCode + ".SS", nil
	case "SZ":
		return meta.StockCode + ".SZ", nil
	case "BJ":
		return "", fmt.Errorf("BJ stock is not supported: %s", meta.StockCode)
	default:
		return "", fmt.Errorf("unsupported stock region %q for code %s", meta.Region, meta.StockCode)
	}
}

func FetchDailyVolumeStock(ctx context.Context, client *http.Client, meta StockMeta, period1 int64, period2 int64) (VolumeStock, error) {
	stocks, err := FetchDailyVolumeStocks(ctx, client, meta, period1, period2, 1)
	if err != nil {
		return VolumeStock{}, err
	}
	if len(stocks) == 0 {
		return VolumeStock{}, fmt.Errorf("no volume stock generated for %s", meta.StockCode)
	}
	return stocks[0], nil
}

func FetchDailyVolumeStocks(ctx context.Context, client *http.Client, meta StockMeta, period1 int64, period2 int64, days int) ([]VolumeStock, error) {
	quote, timestamps, _, err := fetchDailyQuote(ctx, client, meta, period1, period2)
	if err != nil {
		return nil, err
	}
	return buildVolumeStocks(meta, quote, timestamps, days)
}

func buildVolumeStocks(meta StockMeta, quote dailyQuote, timestamps []int64, days int) ([]VolumeStock, error) {
	indexes := latestCompleteIndexes(quote.Close, quote.High, quote.Low, quote.Volume, timestamps, days)
	if len(indexes) == 0 {
		return nil, fmt.Errorf("no complete daily quote found for %s", meta.StockCode)
	}

	stocks := make([]VolumeStock, 0, len(indexes))
	for _, todayIndex := range indexes {
		yesterdayIndex := previousCompleteIndex(quote.Close, quote.Volume, todayIndex)
		if yesterdayIndex < 0 {
			continue
		}

		closePrice := *quote.Close[todayIndex]
		maxPrice := *quote.High[todayIndex]
		minPrice := *quote.Low[todayIndex]
		todayVolume := float64(*quote.Volume[todayIndex])
		if todayVolume < 200000 {
			continue
		}
		yesterdayClose := *quote.Close[yesterdayIndex]
		yesterdayVolume := float64(*quote.Volume[yesterdayIndex])
		amount := todayVolume * closePrice
		if amount < minPoolAmount {
			continue
		}
		volumeRatio := 0.0
		if yesterdayVolume != 0 {
			volumeRatio = todayVolume / yesterdayVolume
		}
		rise := 0.0
		if yesterdayClose != 0 {
			rise = (closePrice - yesterdayClose) / yesterdayClose * 100
		}
		averageVolume10, ok := averagePreviousVolume(quote.Volume, todayIndex, 10)
		if rise <= 0 || volumeRatio <= 3 || !ok || todayVolume < averageVolume10*2 {
			continue
		}

		stocks = append(stocks, VolumeStock{
			StockCode:  meta.StockCode,
			StockName:  meta.StockName,
			SectorID:   meta.SectorID,
			SectorName: meta.SectorName,
			ClosePrice: round(closePrice, 2),
			MaxPrice:   round(maxPrice, 2),
			MinPrice:   round(minPrice, 2),
			Rise:       round(rise, 2),
			Amount:     round(amount, 4),
			Vol:        round(volumeRatio, 2),
			GmtCreate:  quoteTime(timestamps, todayIndex),
		})
	}
	return stocks, nil
}

func FetchDailyShadowStocks(ctx context.Context, client *http.Client, meta StockMeta, period1 int64, period2 int64, days int) ([]ShadowStock, error) {
	quote, timestamps, _, err := fetchDailyQuote(ctx, client, meta, period1, period2)
	if err != nil {
		return nil, err
	}
	return buildShadowStocks(meta, quote, timestamps, days)
}

func buildShadowStocks(meta StockMeta, quote dailyQuote, timestamps []int64, days int) ([]ShadowStock, error) {
	indexes := latestCompleteIndexes(quote.Close, quote.High, quote.Low, quote.Volume, timestamps, days)
	if len(indexes) == 0 {
		return nil, fmt.Errorf("no complete daily quote found for %s", meta.StockCode)
	}

	stocks := make([]ShadowStock, 0, len(indexes))
	for _, todayIndex := range indexes {
		yesterdayIndex := previousCompleteIndex(quote.Close, quote.Volume, todayIndex)
		if yesterdayIndex < 0 {
			continue
		}

		closePrice := *quote.Close[todayIndex]
		if closePrice == 0 {
			continue
		}
		if todayIndex >= len(quote.Open) || quote.Open[todayIndex] == nil {
			continue
		}
		openPrice := *quote.Open[todayIndex]
		if closePrice <= openPrice {
			continue
		}
		maxPrice := *quote.High[todayIndex]
		minPrice := *quote.Low[todayIndex]
		if !higherThanPreviousCloses(quote.Close, todayIndex, maxPrice, 22) {
			continue
		}
		todayVolume := float64(*quote.Volume[todayIndex])
		if todayVolume < 200000 {
			continue
		}
		yesterdayClose := *quote.Close[yesterdayIndex]
		if yesterdayClose == 0 {
			continue
		}
		highRise := (maxPrice - yesterdayClose) / yesterdayClose * 100
		closeRise := (closePrice - yesterdayClose) / yesterdayClose * 100
		if closeRise <= 0 || highRise <= 5 || maxPrice-closePrice <= closePrice-openPrice {
			continue
		}
		amount := todayVolume * closePrice
		if amount < minPoolAmount {
			continue
		}

		stocks = append(stocks, ShadowStock{
			StockCode:  meta.StockCode,
			StockName:  meta.StockName,
			SectorID:   meta.SectorID,
			SectorName: meta.SectorName,
			ClosePrice: round(closePrice, 2),
			MaxPrice:   round(maxPrice, 2),
			MinPrice:   round(minPrice, 2),
			Rise:       round(closeRise, 2),
			Amount:     round(amount, 4),
			Vol:        round(highRise, 2),
			GmtCreate:  quoteTime(timestamps, todayIndex),
		})
	}
	return stocks, nil
}

func buildBreakoutStocks(meta StockMeta, quote dailyQuote, timestamps []int64, days int) ([]BreakoutStock, error) {
	indexes := latestCompleteIndexes(quote.Close, quote.High, quote.Low, quote.Volume, timestamps, days)
	if len(indexes) == 0 {
		return nil, fmt.Errorf("no complete daily quote found for %s", meta.StockCode)
	}

	stocks := make([]BreakoutStock, 0, len(indexes))
	for _, todayIndex := range indexes {
		yesterdayIndex := previousCompleteIndex(quote.Close, quote.Volume, todayIndex)
		if yesterdayIndex < 0 || !hasCompleteHighCloseVolume(quote, todayIndex) {
			continue
		}
		if !recentThreeHighsRise(quote, todayIndex) {
			continue
		}
		if !closeAboveMovingAverages(quote.Close, todayIndex) {
			continue
		}

		closePrice := *quote.Close[todayIndex]
		maxPrice := *quote.High[todayIndex]
		minPrice := *quote.Low[todayIndex]
		todayVolume := float64(*quote.Volume[todayIndex])
		if todayVolume < 200000 {
			continue
		}
		yesterdayClose := *quote.Close[yesterdayIndex]
		if yesterdayClose == 0 || closePrice <= yesterdayClose {
			continue
		}
		closeRise := (closePrice - yesterdayClose) / yesterdayClose * 100
		if closeRise <= 0 {
			continue
		}
		amount := todayVolume * closePrice
		if amount < minPoolAmount {
			continue
		}

		beforeMaxPrice, beforeMaxVol, beforeMaxTime, ok := previousBreakoutHigh(quote, timestamps, todayIndex, 3, 66)
		if !ok || beforeMaxPrice == 0 || beforeMaxVol == 0 {
			continue
		}
		priceDiff := (closePrice - beforeMaxPrice) / beforeMaxPrice
		if priceDiff < -0.03 || priceDiff > 0.07 {
			continue
		}
		volumeRatio := todayVolume / beforeMaxVol
		if volumeRatio > 1.05 {
			continue
		}

		stocks = append(stocks, BreakoutStock{
			StockCode:      meta.StockCode,
			StockName:      meta.StockName,
			SectorID:       meta.SectorID,
			SectorName:     meta.SectorName,
			ClosePrice:     round(closePrice, 2),
			MaxPrice:       round(maxPrice, 2),
			MinPrice:       round(minPrice, 2),
			Rise:           round(closeRise, 2),
			Amount:         round(amount, 4),
			Vol:            round(volumeRatio, 2),
			BeforeMaxPrice: round(beforeMaxPrice, 2),
			BeforeMaxVol:   round(beforeMaxVol, 4),
			BeforeMaxTime:  beforeMaxTime,
			GmtCreate:      quoteTime(timestamps, todayIndex),
		})
	}
	return stocks, nil
}

type dailyQuote struct {
	Close  []*float64
	High   []*float64
	Low    []*float64
	Open   []*float64
	Volume []*int64
}

func fetchDailyQuote(ctx context.Context, client *http.Client, meta StockMeta, period1 int64, period2 int64) (dailyQuote, []int64, string, error) {
	symbol, err := YahooSymbol(meta)
	if err != nil {
		return dailyQuote{}, nil, "", err
	}
	url := fmt.Sprintf(
		"https://query1.finance.yahoo.com/v8/finance/chart/%s?period1=%d&period2=%d&interval=1d&includePrePost=true&events=div%%7Csplit%%7Cearn&lang=en-US&region=US&source=cosaic",
		symbol,
		period1,
		period2,
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return dailyQuote{}, nil, "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")

	resp, err := client.Do(req)
	if err != nil {
		return dailyQuote{}, nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(resp.Body)
		return dailyQuote{}, nil, "", fmt.Errorf("yahoo response status=%s body=%s", resp.Status, string(body))
	}

	var data chartResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return dailyQuote{}, nil, "", err
	}
	if len(data.Chart.Result) == 0 || len(data.Chart.Result[0].Indicators.Quote) == 0 {
		return dailyQuote{}, nil, "", fmt.Errorf("empty yahoo chart result for %s", symbol)
	}

	result := data.Chart.Result[0]
	quote := result.Indicators.Quote[0]
	return dailyQuote{
		Close:  quote.Close,
		High:   quote.High,
		Low:    quote.Low,
		Open:   quote.Open,
		Volume: quote.Volume,
	}, result.Timestamp, symbol, nil
}

func quoteTime(timestamps []int64, index int) time.Time {
	value := time.Now()
	if index < len(timestamps) {
		value = time.Unix(timestamps[index], 0)
	}
	return closeTime(value)
}

func findFirstCoverClose(quote dailyQuote, timestamps []int64, start time.Time, maxPrice float64) (float64, time.Time, bool) {
	startDate := dateOnly(start)
	maxLen := min(len(quote.Close), len(timestamps))
	for i := 0; i < maxLen; i++ {
		if quote.Close[i] == nil {
			continue
		}
		tradeTime := quoteTime(timestamps, i)
		if dateOnly(tradeTime).Before(startDate) {
			continue
		}
		closePrice := *quote.Close[i]
		if closePrice > maxPrice {
			return round(closePrice, 2), tradeTime, true
		}
	}
	return 0, time.Time{}, false
}

func latestClose(quote dailyQuote, timestamps []int64) (float64, time.Time, bool) {
	// Cover tracking intentionally uses the newest Yahoo close, including intraday data before market close.
	maxLen := min(len(quote.Close), len(timestamps))
	for i := maxLen - 1; i >= 0; i-- {
		if quote.Close[i] == nil {
			continue
		}
		return round(*quote.Close[i], 2), quoteTime(timestamps, i), true
	}
	return 0, time.Time{}, false
}

func dateOnly(value time.Time) time.Time {
	location := marketLocation()
	local := value.In(location)
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, location)
}

func CheckFirstStock(ctx context.Context, db *sql.DB) (VolumeStock, error) {
	meta, err := LoadFirstYahooSupportedStock(ctx, db)
	if err != nil {
		return VolumeStock{}, err
	}
	client := &http.Client{Timeout: 20 * time.Second}
	period1, period2 := yahooPeriods(time.Now(), 1)
	return FetchDailyVolumeStock(ctx, client, meta, period1, period2)
}

func latestCompleteIndex(closeValues []*float64, highValues []*float64, lowValues []*float64, volumeValues []*int64) int {
	maxLen := min(len(closeValues), len(highValues), len(lowValues), len(volumeValues))
	for i := maxLen - 1; i >= 0; i-- {
		if closeValues[i] != nil && highValues[i] != nil && lowValues[i] != nil && volumeValues[i] != nil {
			return i
		}
	}
	return -1
}

func latestCompleteIndexes(closeValues []*float64, highValues []*float64, lowValues []*float64, volumeValues []*int64, timestamps []int64, limit int) []int {
	indexes := make([]int, 0, limit)
	maxLen := min(len(closeValues), len(highValues), len(lowValues), len(volumeValues))
	for i := maxLen - 1; i >= 0 && len(indexes) < limit; i-- {
		if closeValues[i] != nil && highValues[i] != nil && lowValues[i] != nil && volumeValues[i] != nil && isFinishedTradeDay(timestamps, i, time.Now()) {
			indexes = append(indexes, i)
		}
	}
	return indexes
}

func isFinishedTradeDay(timestamps []int64, index int, now time.Time) bool {
	location := marketLocation()
	if index >= len(timestamps) {
		return true
	}
	tradeTime := time.Unix(timestamps[index], 0).In(location)
	tradeDate := time.Date(tradeTime.Year(), tradeTime.Month(), tradeTime.Day(), 0, 0, 0, 0, location)
	localNow := now.In(location)
	today := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, location)
	if tradeDate.Before(today) {
		return true
	}
	if tradeDate.After(today) {
		return false
	}
	closeMoment := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 15, 0, 0, 0, location)
	return !localNow.Before(closeMoment)
}

func previousCompleteIndex(closeValues []*float64, volumeValues []*int64, todayIndex int) int {
	maxLen := min(len(closeValues), len(volumeValues))
	if todayIndex > maxLen {
		todayIndex = maxLen
	}
	for i := todayIndex - 1; i >= 0; i-- {
		if closeValues[i] != nil && volumeValues[i] != nil {
			return i
		}
	}
	return -1
}

func higherThanPreviousCloses(closeValues []*float64, todayIndex int, price float64, lookback int) bool {
	count := 0
	for i := todayIndex - 1; i >= 0 && count < lookback; i-- {
		if closeValues[i] == nil {
			continue
		}
		count++
		if price <= *closeValues[i] {
			return false
		}
	}
	return count == lookback
}

func averagePreviousVolume(volumeValues []*int64, todayIndex int, lookback int) (float64, bool) {
	total := 0.0
	count := 0
	for i := todayIndex - 1; i >= 0 && count < lookback; i-- {
		if volumeValues[i] == nil {
			continue
		}
		total += float64(*volumeValues[i])
		count++
	}
	if count != lookback {
		return 0, false
	}
	return total / float64(count), true
}

func hasCompleteHighCloseVolume(quote dailyQuote, index int) bool {
	return index >= 0 &&
		index < len(quote.Close) &&
		index < len(quote.High) &&
		index < len(quote.Volume) &&
		index < len(quote.Low) &&
		quote.Close[index] != nil &&
		quote.High[index] != nil &&
		quote.Low[index] != nil &&
		quote.Volume[index] != nil
}

func recentThreeHighsRise(quote dailyQuote, todayIndex int) bool {
	if todayIndex < 2 {
		return false
	}
	for i := todayIndex - 2; i <= todayIndex; i++ {
		if !hasCompleteHighCloseVolume(quote, i) {
			return false
		}
	}
	return *quote.High[todayIndex-2] < *quote.High[todayIndex-1] &&
		*quote.High[todayIndex-1] < *quote.High[todayIndex] &&
		*quote.Close[todayIndex] > *quote.Close[todayIndex-1]
}

func closeAboveMovingAverages(closeValues []*float64, todayIndex int) bool {
	if todayIndex >= len(closeValues) || closeValues[todayIndex] == nil {
		return false
	}
	ma5, ok := movingAverage(closeValues, todayIndex, 5)
	if !ok {
		return false
	}
	ma10, ok := movingAverage(closeValues, todayIndex, 10)
	if !ok {
		return false
	}
	ma20, ok := movingAverage(closeValues, todayIndex, 20)
	if !ok {
		return false
	}
	closePrice := *closeValues[todayIndex]
	return closePrice > ma5 && ma5 > ma10 && ma10 > ma20
}

func movingAverage(closeValues []*float64, todayIndex int, lookback int) (float64, bool) {
	total := 0.0
	count := 0
	for i := todayIndex; i >= 0 && count < lookback; i-- {
		if closeValues[i] == nil {
			continue
		}
		total += *closeValues[i]
		count++
	}
	if count != lookback {
		return 0, false
	}
	return total / float64(count), true
}

func previousBreakoutHigh(quote dailyQuote, timestamps []int64, todayIndex int, excludeRecent int, windowSize int) (float64, float64, time.Time, bool) {
	start := todayIndex - windowSize + 1
	if start < 0 {
		return 0, 0, time.Time{}, false
	}
	end := todayIndex - excludeRecent + 1
	if end-start <= 3 {
		return 0, 0, time.Time{}, false
	}

	maxIndex := -1
	maxPrice := 0.0
	for i := start; i < end; i++ {
		if !hasCompleteHighCloseVolume(quote, i) {
			continue
		}
		if maxIndex < 0 || *quote.High[i] >= maxPrice {
			maxIndex = i
			maxPrice = *quote.High[i]
		}
	}
	if maxIndex < start+3 || maxIndex < 0 || quote.Volume[maxIndex] == nil {
		return 0, 0, time.Time{}, false
	}
	return maxPrice, float64(*quote.Volume[maxIndex]), quoteTime(timestamps, maxIndex), true
}

func closeTime(value time.Time) time.Time {
	location := marketLocation()
	local := value.In(location)
	return time.Date(local.Year(), local.Month(), local.Day(), 15, 0, 0, 0, location)
}

func marketLocation() *time.Location {
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		return time.FixedZone("Asia/Shanghai", 8*60*60)
	}
	return location
}

func isSTStockName(name string) bool {
	normalized := strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(name), " ", ""))
	return strings.HasPrefix(normalized, "ST") ||
		strings.HasPrefix(normalized, "*ST") ||
		strings.HasPrefix(normalized, "SST") ||
		strings.HasPrefix(normalized, "S*ST")
}

func round(value float64, places int) float64 {
	factor := math.Pow(10, float64(places))
	return math.Round(value*factor) / factor
}
