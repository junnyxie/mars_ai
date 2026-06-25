package yahoo

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"mars_ai/internal/logging"
)

type VolumeRunner struct {
	db      *sql.DB
	client  *http.Client
	running bool
	mu      sync.Mutex
}

type RunResult struct {
	TotalStocks int `json:"total_stocks"`
	Inserted    int `json:"inserted"`
	Skipped     int `json:"skipped"`
	Failed      int `json:"failed"`
}

type CoverRunResult struct {
	TotalRows int `json:"total_rows"`
	Updated   int `json:"updated"`
	Skipped   int `json:"skipped"`
	Failed    int `json:"failed"`
}

type WatchlistRunResult struct {
	TotalRows int `json:"total_rows"`
	Synced    int `json:"synced"`
	Updated   int `json:"updated"`
	Skipped   int `json:"skipped"`
	Failed    int `json:"failed"`
}

type CombinedRunResult struct {
	Volume   RunResult `json:"volume"`
	Shadow   RunResult `json:"shadow"`
	Breakout RunResult `json:"breakout"`
}

type VolumeStockRow struct {
	ID              int64   `json:"id"`
	StockCode       string  `json:"stock_code"`
	StockName       string  `json:"stock_name"`
	SectorID        int64   `json:"sector_id"`
	SectorName      string  `json:"sector_name"`
	ClosePrice      float64 `json:"close_price"`
	MaxPrice        float64 `json:"max_price"`
	MinPrice        float64 `json:"min_price"`
	Rise            float64 `json:"rise"`
	Amount          float64 `json:"amount"`
	Vol             float64 `json:"vol"`
	Start           int     `json:"start"`
	GPTStar         int     `json:"gpt_star"`
	BeforeMaxPrice  float64 `json:"before_max_price"`
	BeforeMaxVol    float64 `json:"before_max_vol"`
	BeforeMaxTime   string  `json:"before_max_time"`
	FirstCoverPrice float64 `json:"first_cover_price"`
	FirstCoverTime  string  `json:"first_cover_time"`
	NowCoverPrice   float64 `json:"now_cover_price"`
	NowCoverTime    string  `json:"now_cover_time"`
	GmtCreate       string  `json:"gmt_create"`
}

type ShadowCoverRow struct {
	ID              int64
	StockCode       string
	StockName       string
	Region          string
	SectorID        int64
	SectorName      sql.NullString
	MaxPrice        float64
	GmtCreate       time.Time
	FirstCoverPrice sql.NullFloat64
}

type StockPoolPage struct {
	Rows     []VolumeStockRow `json:"rows"`
	Total    int              `json:"total"`
	Page     int              `json:"page"`
	PageSize int              `json:"page_size"`
}

type WatchlistStockRow struct {
	ID           int64   `json:"id"`
	SourcePool   string  `json:"source_pool"`
	SourceID     int64   `json:"source_id"`
	StockCode    string  `json:"stock_code"`
	StockName    string  `json:"stock_name"`
	SectorID     int64   `json:"sector_id"`
	SectorName   string  `json:"sector_name"`
	JoinTime     string  `json:"join_time"`
	JoinPrice    float64 `json:"join_price"`
	CurrentPrice float64 `json:"current_price"`
	CurrentTime  string  `json:"current_time"`
	Rise         float64 `json:"rise"`
	GmtCreate    string  `json:"gmt_create"`
}

type WatchlistStockPage struct {
	Rows     []WatchlistStockRow `json:"rows"`
	Total    int                 `json:"total"`
	Page     int                 `json:"page"`
	PageSize int                 `json:"page_size"`
}

type watchlistUpdateRow struct {
	ID         int64
	StockCode  string
	StockName  string
	Region     string
	SectorID   int64
	SectorName sql.NullString
	JoinPrice  float64
}

type deleteStockPoolRowsRequest struct {
	IDs []int64 `json:"ids"`
}

type deleteStockPoolRowsResponse struct {
	Deleted int64 `json:"deleted"`
}

type updateStockPoolStartRequest struct {
	ID    int64 `json:"id"`
	Start int   `json:"start"`
}

type updateStockPoolGPTStarRequest struct {
	ID      int64 `json:"id"`
	GPTStar int   `json:"gpt_star"`
}

func NewVolumeRunner(db *sql.DB) *VolumeRunner {
	return &VolumeRunner{
		db:     db,
		client: &http.Client{Timeout: 20 * time.Second},
	}
}

func (r *VolumeRunner) Run(ctx context.Context) (RunResult, error) {
	return r.RunDays(ctx, 1)
}

func (r *VolumeRunner) RunMacroMarketDays(ctx context.Context, days int) (MacroMarketPreview, error) {
	if days < 1 || days > 60 {
		return MacroMarketPreview{}, fmt.Errorf("days must be between 1 and 60")
	}
	if err := ensureMacroMarketDailyTable(ctx, r.db); err != nil {
		return MacroMarketPreview{}, err
	}
	result := FetchMacroMarketDays(ctx, r.client, time.Now(), days)
	for index, item := range result.Rows {
		if item.Error != "" {
			log.Printf("[macro] skip symbol=%s name=%s date=%s err=%s", item.Symbol, item.MarketName, item.TradeDate, item.Error)
			continue
		}
		inserted, err := insertMacroMarketItem(ctx, r.db, item)
		if err != nil {
			result.Failed++
			result.Rows[index].Error = err.Error()
			log.Printf("[macro] insert failed symbol=%s name=%s date=%s err=%v", item.Symbol, item.MarketName, item.TradeDate, err)
			continue
		}
		if inserted {
			result.Inserted++
		} else {
			result.Updated++
		}
		log.Printf("[macro] saved symbol=%s name=%s date=%s close=%.4f rise=%.4f inserted=%t", item.Symbol, item.MarketName, item.TradeDate, item.ClosePrice, item.Rise, inserted)
	}
	return result, nil
}

func (r *VolumeRunner) RunDays(ctx context.Context, days int) (RunResult, error) {
	if days < 1 || days > 60 {
		return RunResult{}, fmt.Errorf("days must be between 1 and 60")
	}
	r.mu.Lock()
	if r.running {
		r.mu.Unlock()
		return RunResult{}, fmt.Errorf("volume job is already running")
	}
	r.running = true
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		r.running = false
		r.mu.Unlock()
	}()

	if err := ensureVolumeStockTable(ctx, r.db); err != nil {
		return RunResult{}, err
	}

	stocks, err := LoadYahooSupportedStocks(ctx, r.db)
	if err != nil {
		return RunResult{}, err
	}

	period1, period2 := yahooPeriods(time.Now(), days)
	result := RunResult{TotalStocks: len(stocks)}
	for index, meta := range stocks {
		if index > 0 {
			time.Sleep(100 * time.Millisecond)
		}
		volumes, err := FetchDailyVolumeStocks(ctx, r.client, meta, period1, period2, days)
		if err != nil {
			result.Failed++
			log.Printf("[volume] fetch failed stock_code=%s stock_name=%s err=%v", meta.StockCode, meta.StockName, err)
			logRunProgress("volume", index+1, len(stocks), meta, result)
			continue
		}
		r.insertVolumeStocks(ctx, volumes, &result)
		logRunProgress("volume", index+1, len(stocks), meta, result)
	}
	return result, nil
}

func (r *VolumeRunner) RunShadowDays(ctx context.Context, days int) (RunResult, error) {
	if days < 1 || days > 60 {
		return RunResult{}, fmt.Errorf("days must be between 1 and 60")
	}
	r.mu.Lock()
	if r.running {
		r.mu.Unlock()
		return RunResult{}, fmt.Errorf("stock pool job is already running")
	}
	r.running = true
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		r.running = false
		r.mu.Unlock()
	}()

	if err := ensureShadowStockTable(ctx, r.db); err != nil {
		return RunResult{}, err
	}

	stocks, err := LoadYahooSupportedStocks(ctx, r.db)
	if err != nil {
		return RunResult{}, err
	}

	period1, period2 := yahooPeriods(time.Now(), days)
	result := RunResult{TotalStocks: len(stocks)}
	for index, meta := range stocks {
		if index > 0 {
			time.Sleep(100 * time.Millisecond)
		}
		shadows, err := FetchDailyShadowStocks(ctx, r.client, meta, period1, period2, days)
		if err != nil {
			result.Failed++
			log.Printf("[shadow] fetch failed stock_code=%s stock_name=%s err=%v", meta.StockCode, meta.StockName, err)
			logRunProgress("shadow", index+1, len(stocks), meta, result)
			continue
		}
		r.insertShadowStocks(ctx, shadows, &result)
		logRunProgress("shadow", index+1, len(stocks), meta, result)
	}
	return result, nil
}

func (r *VolumeRunner) RunBreakoutDays(ctx context.Context, days int) (RunResult, error) {
	if days < 1 || days > 60 {
		return RunResult{}, fmt.Errorf("days must be between 1 and 60")
	}
	log.Printf("[breakout] run start days=%d", days)
	r.mu.Lock()
	if r.running {
		r.mu.Unlock()
		return RunResult{}, fmt.Errorf("stock pool job is already running")
	}
	r.running = true
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		r.running = false
		r.mu.Unlock()
	}()

	if err := ensureBreakoutStockTable(ctx, r.db); err != nil {
		return RunResult{}, err
	}

	stocks, err := LoadYahooSupportedStocks(ctx, r.db)
	if err != nil {
		return RunResult{}, err
	}
	log.Printf("[breakout] loaded stocks total=%d", len(stocks))

	period1, period2 := yahooPeriods(time.Now(), days)
	log.Printf("[breakout] yahoo period period1=%d period2=%d days=%d", period1, period2, days)
	result := RunResult{TotalStocks: len(stocks)}
	for index, meta := range stocks {
		if index > 0 {
			time.Sleep(100 * time.Millisecond)
		}
		quote, timestamps, _, err := fetchDailyQuote(ctx, r.client, meta, period1, period2)
		if err != nil {
			result.Failed++
			log.Printf("[breakout] fetch failed stock_code=%s stock_name=%s err=%v", meta.StockCode, meta.StockName, err)
			logRunProgress("breakout", index+1, len(stocks), meta, result)
			continue
		}
		breakouts, err := buildBreakoutStocks(meta, quote, timestamps, days)
		if err != nil {
			result.Failed++
			log.Printf("[breakout] build failed stock_code=%s stock_name=%s err=%v", meta.StockCode, meta.StockName, err)
			logRunProgress("breakout", index+1, len(stocks), meta, result)
			continue
		}
		r.insertBreakoutStocks(ctx, breakouts, &result)
		logRunProgress("breakout", index+1, len(stocks), meta, result)
	}
	return result, nil
}

func (r *VolumeRunner) insertVolumeStocks(ctx context.Context, volumes []VolumeStock, result *RunResult) {
	if len(volumes) == 0 {
		result.Skipped++
		return
	}
	for _, volume := range volumes {
		if volume.Vol <= 3 {
			result.Skipped++
			log.Printf("[volume] skip stock_code=%s stock_name=%s date=%s vol=%.2f", volume.StockCode, volume.StockName, volume.GmtCreate.Format("2006-01-02"), volume.Vol)
			continue
		}
		if err := InsertVolumeStock(ctx, r.db, volume); err != nil {
			result.Failed++
			log.Printf("[volume] insert failed stock_code=%s stock_name=%s date=%s err=%v", volume.StockCode, volume.StockName, volume.GmtCreate.Format("2006-01-02"), err)
			continue
		}
		result.Inserted++
		log.Printf("[volume] inserted and committed stock_code=%s stock_name=%s date=%s vol=%.2f amount=%.4f", volume.StockCode, volume.StockName, volume.GmtCreate.Format("2006-01-02"), volume.Vol, volume.Amount)
	}
}

func (r *VolumeRunner) insertShadowStocks(ctx context.Context, shadows []ShadowStock, result *RunResult) {
	if len(shadows) == 0 {
		result.Skipped++
		return
	}
	for _, shadow := range shadows {
		if err := InsertShadowStock(ctx, r.db, shadow); err != nil {
			result.Failed++
			log.Printf("[shadow] insert failed stock_code=%s stock_name=%s date=%s err=%v", shadow.StockCode, shadow.StockName, shadow.GmtCreate.Format("2006-01-02"), err)
			continue
		}
		result.Inserted++
		log.Printf("[shadow] inserted and committed stock_code=%s stock_name=%s date=%s shadow=%.2f amount=%.4f", shadow.StockCode, shadow.StockName, shadow.GmtCreate.Format("2006-01-02"), shadow.Vol, shadow.Amount)
	}
}

func (r *VolumeRunner) insertBreakoutStocks(ctx context.Context, breakouts []BreakoutStock, result *RunResult) {
	if len(breakouts) == 0 {
		result.Skipped++
		return
	}
	for _, breakout := range breakouts {
		if err := InsertBreakoutStock(ctx, r.db, breakout); err != nil {
			result.Failed++
			log.Printf("[breakout] insert failed stock_code=%s stock_name=%s date=%s err=%v", breakout.StockCode, breakout.StockName, breakout.GmtCreate.Format("2006-01-02"), err)
			continue
		}
		result.Inserted++
		log.Printf("[breakout] inserted and committed stock_code=%s stock_name=%s date=%s vol=%.2f before_max=%.2f before_max_time=%s amount=%.4f", breakout.StockCode, breakout.StockName, breakout.GmtCreate.Format("2006-01-02"), breakout.Vol, breakout.BeforeMaxPrice, breakout.BeforeMaxTime.Format("2006-01-02"), breakout.Amount)
	}
}

func logRunProgress(name string, current int, total int, meta StockMeta, result RunResult) {
	log.Printf("[%s] progress current=%d total=%d stock_code=%s stock_name=%s region=%s sector_id=%d sector_name=%s inserted=%d skipped=%d failed=%d",
		name,
		current,
		total,
		meta.StockCode,
		meta.StockName,
		meta.Region,
		meta.SectorID,
		stockMetaSectorName(meta),
		result.Inserted,
		result.Skipped,
		result.Failed,
	)
}

func logCombinedRunProgress(name string, current int, total int, meta StockMeta, result CombinedRunResult) {
	log.Printf("[%s] progress current=%d total=%d stock_code=%s stock_name=%s region=%s sector_id=%d sector_name=%s volume={inserted=%d skipped=%d failed=%d} shadow={inserted=%d skipped=%d failed=%d} breakout={inserted=%d skipped=%d failed=%d}",
		name,
		current,
		total,
		meta.StockCode,
		meta.StockName,
		meta.Region,
		meta.SectorID,
		stockMetaSectorName(meta),
		result.Volume.Inserted,
		result.Volume.Skipped,
		result.Volume.Failed,
		result.Shadow.Inserted,
		result.Shadow.Skipped,
		result.Shadow.Failed,
		result.Breakout.Inserted,
		result.Breakout.Skipped,
		result.Breakout.Failed,
	)
}

func stockMetaSectorName(meta StockMeta) string {
	if meta.SectorName.Valid {
		return meta.SectorName.String
	}
	return ""
}

func (r *VolumeRunner) RunAllDays(ctx context.Context, days int) (CombinedRunResult, error) {
	if days < 1 || days > 60 {
		return CombinedRunResult{}, fmt.Errorf("days must be between 1 and 60")
	}
	r.mu.Lock()
	if r.running {
		r.mu.Unlock()
		return CombinedRunResult{}, fmt.Errorf("stock pool job is already running")
	}
	r.running = true
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		r.running = false
		r.mu.Unlock()
	}()

	if err := ensureVolumeStockTable(ctx, r.db); err != nil {
		return CombinedRunResult{}, err
	}
	if err := ensureShadowStockTable(ctx, r.db); err != nil {
		return CombinedRunResult{}, err
	}
	if err := ensureBreakoutStockTable(ctx, r.db); err != nil {
		return CombinedRunResult{}, err
	}

	stocks, err := LoadYahooSupportedStocks(ctx, r.db)
	if err != nil {
		return CombinedRunResult{}, err
	}

	period1, period2 := yahooPeriods(time.Now(), days)
	result := CombinedRunResult{
		Volume:   RunResult{TotalStocks: len(stocks)},
		Shadow:   RunResult{TotalStocks: len(stocks)},
		Breakout: RunResult{TotalStocks: len(stocks)},
	}
	for index, meta := range stocks {
		if index > 0 {
			time.Sleep(100 * time.Millisecond)
		}

		quote, timestamps, _, err := fetchDailyQuote(ctx, r.client, meta, period1, period2)
		if err != nil {
			result.Volume.Failed++
			result.Shadow.Failed++
			result.Breakout.Failed++
			log.Printf("[stock-pool] fetch failed stock_code=%s stock_name=%s err=%v", meta.StockCode, meta.StockName, err)
			logCombinedRunProgress("stock-pool", index+1, len(stocks), meta, result)
			continue
		}

		volumes, err := buildVolumeStocks(meta, quote, timestamps, days)
		if err != nil {
			result.Volume.Failed++
			log.Printf("[volume] build failed stock_code=%s stock_name=%s err=%v", meta.StockCode, meta.StockName, err)
		} else {
			r.insertVolumeStocks(ctx, volumes, &result.Volume)
		}

		shadows, err := buildShadowStocks(meta, quote, timestamps, days)
		if err != nil {
			result.Shadow.Failed++
			log.Printf("[shadow] build failed stock_code=%s stock_name=%s err=%v", meta.StockCode, meta.StockName, err)
		} else {
			r.insertShadowStocks(ctx, shadows, &result.Shadow)
		}

		breakouts, err := buildBreakoutStocks(meta, quote, timestamps, days)
		if err != nil {
			result.Breakout.Failed++
			log.Printf("[breakout] build failed stock_code=%s stock_name=%s err=%v", meta.StockCode, meta.StockName, err)
		} else {
			r.insertBreakoutStocks(ctx, breakouts, &result.Breakout)
		}
		logCombinedRunProgress("stock-pool", index+1, len(stocks), meta, result)
	}
	return result, nil
}

func (r *VolumeRunner) RunShadowCover(ctx context.Context) (CoverRunResult, error) {
	return r.RunShadowCoverIDs(ctx, nil)
}

func (r *VolumeRunner) RunShadowCoverIDs(ctx context.Context, ids []int64) (CoverRunResult, error) {
	r.mu.Lock()
	if r.running {
		r.mu.Unlock()
		return CoverRunResult{}, fmt.Errorf("stock pool job is already running")
	}
	r.running = true
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		r.running = false
		r.mu.Unlock()
	}()

	if err := ensureShadowStockTable(ctx, r.db); err != nil {
		return CoverRunResult{}, err
	}
	rows, err := LoadShadowCoverRows(ctx, r.db, ids)
	if err != nil {
		return CoverRunResult{}, err
	}

	result := CoverRunResult{TotalRows: len(rows)}
	period1, period2 := shadowCoverPeriods(time.Now())
	quoteCache := make(map[string]struct {
		quote      dailyQuote
		timestamps []int64
	})
	requestCount := 0
	for _, row := range rows {
		meta := StockMeta{
			StockCode:  row.StockCode,
			StockName:  row.StockName,
			Region:     row.Region,
			SectorID:   row.SectorID,
			SectorName: row.SectorName,
		}
		cacheKey := row.Region + ":" + row.StockCode
		cached, ok := quoteCache[cacheKey]
		if !ok {
			if requestCount > 0 {
				time.Sleep(100 * time.Millisecond)
			}
			quote, timestamps, _, err := fetchDailyQuote(ctx, r.client, meta, period1, period2)
			if err != nil {
				result.Failed++
				log.Printf("[shadow-cover] fetch failed id=%d stock_code=%s stock_name=%s err=%v", row.ID, row.StockCode, row.StockName, err)
				continue
			}
			requestCount++
			cached = struct {
				quote      dailyQuote
				timestamps []int64
			}{quote: quote, timestamps: timestamps}
			quoteCache[cacheKey] = cached
		}

		if !row.FirstCoverPrice.Valid {
			price, coverTime, ok := findFirstCoverClose(cached.quote, cached.timestamps, row.GmtCreate, row.MaxPrice)
			if !ok {
				result.Skipped++
				continue
			}
			nowPrice, nowTime, ok := latestClose(cached.quote, cached.timestamps)
			if !ok {
				result.Skipped++
				continue
			}
			if err := UpdateShadowFirstCover(ctx, r.db, row.ID, price, coverTime, nowPrice, nowTime); err != nil {
				result.Failed++
				log.Printf("[shadow-cover] update first cover failed id=%d stock_code=%s err=%v", row.ID, row.StockCode, err)
				continue
			}
			result.Updated++
			log.Printf("[shadow-cover] first cover updated id=%d stock_code=%s first_price=%.2f first_time=%s now_price=%.2f now_time=%s", row.ID, row.StockCode, price, coverTime.Format("2006-01-02"), nowPrice, nowTime.Format("2006-01-02"))
			continue
		}

		price, coverTime, ok := latestClose(cached.quote, cached.timestamps)
		if !ok {
			result.Skipped++
			continue
		}
		if err := UpdateShadowNowCover(ctx, r.db, row.ID, price, coverTime); err != nil {
			result.Failed++
			log.Printf("[shadow-cover] update now cover failed id=%d stock_code=%s err=%v", row.ID, row.StockCode, err)
			continue
		}
		result.Updated++
		log.Printf("[shadow-cover] now cover updated id=%d stock_code=%s price=%.2f time=%s", row.ID, row.StockCode, price, coverTime.Format("2006-01-02"))
	}
	return result, nil
}

func (r *VolumeRunner) RunWatchlist(ctx context.Context) (WatchlistRunResult, error) {
	r.mu.Lock()
	if r.running {
		r.mu.Unlock()
		return WatchlistRunResult{}, fmt.Errorf("stock pool job is already running")
	}
	r.running = true
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		r.running = false
		r.mu.Unlock()
	}()

	if err := ensureWatchlistStockTable(ctx, r.db); err != nil {
		return WatchlistRunResult{}, err
	}
	result, err := SyncWatchlistCandidates(ctx, r.db)
	if err != nil {
		return result, err
	}
	updateResult, err := r.UpdateWatchlistPrices(ctx)
	result.TotalRows = updateResult.TotalRows
	result.Updated = updateResult.Updated
	result.Skipped += updateResult.Skipped
	result.Failed += updateResult.Failed
	if err != nil {
		return result, err
	}
	return result, nil
}

func (r *VolumeRunner) UpdateWatchlistPrices(ctx context.Context) (WatchlistRunResult, error) {
	return r.UpdateWatchlistPriceIDs(ctx, nil)
}

func (r *VolumeRunner) UpdateWatchlistPriceIDs(ctx context.Context, ids []int64) (WatchlistRunResult, error) {
	if err := ensureWatchlistStockTable(ctx, r.db); err != nil {
		return WatchlistRunResult{}, err
	}
	rows, err := LoadWatchlistUpdateRows(ctx, r.db, ids)
	if err != nil {
		return WatchlistRunResult{}, err
	}
	result := WatchlistRunResult{TotalRows: len(rows)}
	period1, period2 := shadowCoverPeriods(time.Now())
	quoteCache := make(map[string]struct {
		quote      dailyQuote
		timestamps []int64
	})
	requestCount := 0
	for _, row := range rows {
		if row.JoinPrice <= 0 {
			result.Skipped++
			continue
		}
		meta := StockMeta{
			StockCode:  row.StockCode,
			StockName:  row.StockName,
			Region:     row.Region,
			SectorID:   row.SectorID,
			SectorName: row.SectorName,
		}
		cacheKey := row.Region + ":" + row.StockCode
		cached, ok := quoteCache[cacheKey]
		if !ok {
			if requestCount > 0 {
				time.Sleep(100 * time.Millisecond)
			}
			quote, timestamps, _, err := fetchDailyQuote(ctx, r.client, meta, period1, period2)
			if err != nil {
				result.Failed++
				log.Printf("[watchlist] fetch failed id=%d stock_code=%s stock_name=%s err=%v", row.ID, row.StockCode, row.StockName, err)
				continue
			}
			requestCount++
			cached = struct {
				quote      dailyQuote
				timestamps []int64
			}{quote: quote, timestamps: timestamps}
			quoteCache[cacheKey] = cached
		}
		price, priceTime, ok := latestClose(cached.quote, cached.timestamps)
		if !ok {
			result.Skipped++
			continue
		}
		rise := round((price-row.JoinPrice)/row.JoinPrice*100, 4)
		if err := UpdateWatchlistPrice(ctx, r.db, row.ID, price, priceTime, rise); err != nil {
			result.Failed++
			log.Printf("[watchlist] update failed id=%d stock_code=%s stock_name=%s err=%v", row.ID, row.StockCode, row.StockName, err)
			continue
		}
		result.Updated++
		log.Printf("[watchlist] updated id=%d stock_code=%s stock_name=%s current_price=%.2f current_time=%s rise=%.2f", row.ID, row.StockCode, row.StockName, price, priceTime.Format("2006-01-02"), rise)
	}
	return result, nil
}

func (r *VolumeRunner) ServeHTTP(addr string) error {
	webLogger, webLogWriter, err := logging.NewWebLogger()
	if err != nil {
		return fmt.Errorf("init web logger failed: %w", err)
	}
	defer webLogWriter.Close()

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/volume/run", func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodPost && req.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if r.IsRunning() {
			http.Error(w, "volume job is already running", http.StatusConflict)
			return
		}
		days, err := parseRunDays(req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
			defer cancel()
			result, err := r.RunDays(ctx, days)
			if err != nil {
				log.Printf("[volume] async run failed: %v", err)
				return
			}
			log.Printf("[volume] async run done days=%d result=%+v", days, result)
		}()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "started", "days": days})
	})
	mux.HandleFunc("/shadow/run", func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodPost && req.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if r.IsRunning() {
			http.Error(w, "stock pool job is already running", http.StatusConflict)
			return
		}
		days, err := parseRunDays(req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
			defer cancel()
			result, err := r.RunShadowDays(ctx, days)
			if err != nil {
				log.Printf("[shadow] async run failed: %v", err)
				return
			}
			log.Printf("[shadow] async run done days=%d result=%+v", days, result)
		}()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "started", "days": days})
	})
	mux.HandleFunc("/shadow/cover/run", func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodPost && req.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if r.IsRunning() {
			http.Error(w, "stock pool job is already running", http.StatusConflict)
			return
		}
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
			defer cancel()
			result, err := r.RunShadowCover(ctx)
			if err != nil {
				log.Printf("[shadow-cover] async run failed: %v", err)
				return
			}
			log.Printf("[shadow-cover] async run done result=%+v", result)
			watchlistResult, err := r.RunWatchlist(ctx)
			if err != nil {
				log.Printf("[watchlist] async run after shadow-cover failed: %v", err)
				return
			}
			log.Printf("[watchlist] async run after shadow-cover done result=%+v", watchlistResult)
		}()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "started"})
	})
	mux.HandleFunc("/watchlist/run", func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodPost && req.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if r.IsRunning() {
			http.Error(w, "stock pool job is already running", http.StatusConflict)
			return
		}
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
			defer cancel()
			result, err := r.RunWatchlist(ctx)
			if err != nil {
				log.Printf("[watchlist] async run failed: %v", err)
				return
			}
			log.Printf("[watchlist] async run done result=%+v", result)
		}()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "started"})
	})
	mux.HandleFunc("/stock-pool/run", func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodPost && req.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if r.IsRunning() {
			http.Error(w, "stock pool job is already running", http.StatusConflict)
			return
		}
		days, err := parseRunDays(req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
			defer cancel()
			result, err := r.RunAllDays(ctx, days)
			if err != nil {
				log.Printf("[stock-pool] async run failed: %v", err)
				return
			}
			log.Printf("[stock-pool] async run done days=%d result=%+v", days, result)
		}()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "started", "days": days})
	})
	mux.HandleFunc("/breakout/run", func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodPost && req.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if r.IsRunning() {
			http.Error(w, "stock pool job is already running", http.StatusConflict)
			return
		}
		days, err := parseRunDays(req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		log.Printf("[breakout] run request accepted method=%s path=%s remote=%s days=%d", req.Method, req.URL.String(), req.RemoteAddr, days)
		go func() {
			log.Printf("[breakout] async run started days=%d", days)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
			defer cancel()
			result, err := r.RunBreakoutDays(ctx, days)
			if err != nil {
				log.Printf("[breakout] async run failed: %v", err)
				return
			}
			log.Printf("[breakout] async run done days=%d result=%+v", days, result)
		}()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "started", "days": days})
	})
	mux.HandleFunc("/api/volume-stocks", r.handleVolumeStocks)
	mux.HandleFunc("/api/shadow-stocks", r.handleShadowStocks)
	mux.HandleFunc("/api/breakout-stocks", r.handleBreakoutStocks)
	mux.HandleFunc("/api/watchlist-stocks", r.handleWatchlistStocks)
	mux.HandleFunc("/api/stock-pool/export", r.handleExportStockPool)
	mux.HandleFunc("/api/macro-market/day", r.handleMacroMarketDay)
	mux.HandleFunc("/api/macro-market", r.handleMacroMarket)
	mux.HandleFunc("/api/macro-market/preview", r.handleMacroMarketPreview)
	mux.HandleFunc("/api/macro-market/run", r.handleMacroMarketPreview)
	mux.HandleFunc("/api/volume-stocks/delete", r.handleDeleteVolumeStocks)
	mux.HandleFunc("/api/shadow-stocks/delete", r.handleDeleteShadowStocks)
	mux.HandleFunc("/api/breakout-stocks/delete", r.handleDeleteBreakoutStocks)
	mux.HandleFunc("/api/volume-stocks/start", r.handleUpdateVolumeStart)
	mux.HandleFunc("/api/shadow-stocks/start", r.handleUpdateShadowStart)
	mux.HandleFunc("/api/breakout-stocks/start", r.handleUpdateBreakoutStart)
	mux.HandleFunc("/api/volume-stocks/gpt-star", r.handleUpdateVolumeGPTStar)
	mux.HandleFunc("/api/shadow-stocks/gpt-star", r.handleUpdateShadowGPTStar)
	mux.HandleFunc("/api/breakout-stocks/gpt-star", r.handleUpdateBreakoutGPTStar)
	mux.HandleFunc("/api/shadow-stocks/cover", r.handleRunShadowCoverIDs)
	mux.HandleFunc("/api/watchlist-stocks/refresh", r.handleRefreshWatchlistIDs)
	mux.HandleFunc("/api/watchlist-stocks/delete", r.handleDeleteWatchlistStocks)
	mux.Handle("/", http.FileServer(http.Dir("web")))

	go r.scheduleDaily()
	go r.scheduleMacroDaily()
	log.Printf("[server] listen addr=%s", addr)
	return http.ListenAndServe(addr, withWebLog(mux, webLogger))
}

func parseRunDays(req *http.Request) (int, error) {
	value := req.URL.Query().Get("days")
	if value == "" {
		return 1, nil
	}
	days, err := strconv.Atoi(value)
	if err != nil || days < 1 || days > 60 {
		return 0, fmt.Errorf("days must be between 1 and 60")
	}
	return days, nil
}

func (r *VolumeRunner) IsRunning() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.running
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func withWebLog(next http.Handler, logger *log.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		start := time.Now()
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(recorder, req)
		logger.Printf(
			"method=%s path=%s remote=%s status=%d duration_ms=%d",
			req.Method,
			req.URL.RequestURI(),
			req.RemoteAddr,
			recorder.status,
			time.Since(start).Milliseconds(),
		)
	})
}

func (r *VolumeRunner) handleVolumeStocks(w http.ResponseWriter, req *http.Request) {
	page, err := queryStockPoolRows(req.Context(), r.db, req, "volume_stock")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(page)
}

func (r *VolumeRunner) handleShadowStocks(w http.ResponseWriter, req *http.Request) {
	page, err := queryStockPoolRows(req.Context(), r.db, req, "shadow_stock")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(page)
}

func (r *VolumeRunner) handleBreakoutStocks(w http.ResponseWriter, req *http.Request) {
	page, err := queryStockPoolRows(req.Context(), r.db, req, "breakout_stock")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(page)
}

func (r *VolumeRunner) handleWatchlistStocks(w http.ResponseWriter, req *http.Request) {
	page, err := queryWatchlistRows(req.Context(), r.db, req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(page)
}

func (r *VolumeRunner) handleExportStockPool(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	pool := req.URL.Query().Get("pool")
	table, err := stockPoolTableByName(pool)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	page, err := queryStockPoolRows(req.Context(), r.db, req, table)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	markdown := buildStockPoolExportMarkdown(pool, req, page.Rows)
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	_, _ = w.Write([]byte(markdown))
}

func (r *VolumeRunner) handleMacroMarketPreview(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet && req.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	days, err := parseRunDays(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(req.Context(), 2*time.Minute)
	defer cancel()
	result, err := r.RunMacroMarketDays(ctx, days)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}

func (r *VolumeRunner) handleMacroMarket(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	limit := 60
	if value := req.URL.Query().Get("limit"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed <= 0 || parsed > 120 {
			http.Error(w, "limit must be between 1 and 120", http.StatusBadRequest)
			return
		}
		limit = parsed
	}
	page, err := queryMacroMarketPage(req.Context(), r.db, limit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(page)
}

func (r *VolumeRunner) handleMacroMarketDay(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	tradeDate := req.URL.Query().Get("date")
	snapshot, err := queryMacroMarketSnapshot(req.Context(), r.db, tradeDate)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "macro market date not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(snapshot)
}

func (r *VolumeRunner) handleDeleteVolumeStocks(w http.ResponseWriter, req *http.Request) {
	r.handleDeleteStockPoolRows(w, req, "volume_stock")
}

func (r *VolumeRunner) handleDeleteShadowStocks(w http.ResponseWriter, req *http.Request) {
	r.handleDeleteStockPoolRows(w, req, "shadow_stock")
}

func (r *VolumeRunner) handleDeleteBreakoutStocks(w http.ResponseWriter, req *http.Request) {
	r.handleDeleteStockPoolRows(w, req, "breakout_stock")
}

func (r *VolumeRunner) handleRunShadowCoverIDs(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var payload deleteStockPoolRowsRequest
	if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}
	ids, err := normalizeIDs(payload.IDs)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(req.Context(), 2*time.Hour)
	defer cancel()
	result, err := r.RunShadowCoverIDs(ctx, ids)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	for _, id := range ids {
		if err := SyncWatchlistFromPoolRow(req.Context(), r.db, "shadow_stock", id); err != nil {
			log.Printf("[watchlist] sync after selected shadow cover failed id=%d err=%v", id, err)
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}

func (r *VolumeRunner) handleRefreshWatchlistIDs(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if r.IsRunning() {
		http.Error(w, "stock pool job is already running", http.StatusConflict)
		return
	}
	var payload deleteStockPoolRowsRequest
	if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}
	ids, err := normalizeIDs(payload.IDs)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(req.Context(), 2*time.Hour)
	defer cancel()
	result, err := r.UpdateWatchlistPriceIDs(ctx, ids)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}

func (r *VolumeRunner) handleDeleteWatchlistStocks(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var payload deleteStockPoolRowsRequest
	if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}
	deleted, err := DeleteWatchlistRows(req.Context(), r.db, payload.IDs)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(deleteStockPoolRowsResponse{Deleted: deleted})
}

func (r *VolumeRunner) handleUpdateVolumeStart(w http.ResponseWriter, req *http.Request) {
	r.handleUpdateStockPoolStart(w, req, "volume_stock")
}

func (r *VolumeRunner) handleUpdateShadowStart(w http.ResponseWriter, req *http.Request) {
	r.handleUpdateStockPoolStart(w, req, "shadow_stock")
}

func (r *VolumeRunner) handleUpdateBreakoutStart(w http.ResponseWriter, req *http.Request) {
	r.handleUpdateStockPoolStart(w, req, "breakout_stock")
}

func (r *VolumeRunner) handleUpdateVolumeGPTStar(w http.ResponseWriter, req *http.Request) {
	r.handleUpdateStockPoolGPTStar(w, req, "volume_stock")
}

func (r *VolumeRunner) handleUpdateShadowGPTStar(w http.ResponseWriter, req *http.Request) {
	r.handleUpdateStockPoolGPTStar(w, req, "shadow_stock")
}

func (r *VolumeRunner) handleUpdateBreakoutGPTStar(w http.ResponseWriter, req *http.Request) {
	r.handleUpdateStockPoolGPTStar(w, req, "breakout_stock")
}

func (r *VolumeRunner) handleUpdateStockPoolStart(w http.ResponseWriter, req *http.Request, table string) {
	if req.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var payload updateStockPoolStartRequest
	if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}
	if payload.Start != 0 {
		payload.Start = 1
	}
	if err := UpdateStockPoolStart(req.Context(), r.db, table, payload.ID, payload.Start); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"id": payload.ID, "start": payload.Start})
}

func (r *VolumeRunner) handleUpdateStockPoolGPTStar(w http.ResponseWriter, req *http.Request, table string) {
	if req.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var payload updateStockPoolGPTStarRequest
	if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}
	if payload.GPTStar != 0 {
		payload.GPTStar = 1
	}
	if err := UpdateStockPoolGPTStar(req.Context(), r.db, table, payload.ID, payload.GPTStar); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"id": payload.ID, "gpt_star": payload.GPTStar})
}

func (r *VolumeRunner) handleDeleteStockPoolRows(w http.ResponseWriter, req *http.Request, table string) {
	if req.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var payload deleteStockPoolRowsRequest
	if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}
	deleted, err := DeleteStockPoolRows(req.Context(), r.db, table, payload.IDs)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(deleteStockPoolRowsResponse{Deleted: deleted})
}

func QueryVolumeStocks(ctx context.Context, db *sql.DB, req *http.Request) (StockPoolPage, error) {
	return queryStockPoolRows(ctx, db, req, "volume_stock")
}

func buildStockPoolExportMarkdown(pool string, req *http.Request, rows []VolumeStockRow) string {
	var builder strings.Builder
	title := stockPoolTitle(pool)
	query := req.URL.Query()
	fmt.Fprintf(&builder, "# %s基本面初筛\n\n", title)
	builder.WriteString("下面是我的 A 股技术形态股票池，请你只做基本面和事件风险初筛，不要给买卖建议。\n\n")
	builder.WriteString("请从主营业务、业绩稳定性、估值水平、行业景气度、公告风险、解禁减持风险、财务质量、技术形态与基本面匹配度几个角度分析。\n")
	builder.WriteString("最后请给出三个等级之一：优先研究、谨慎观察、直接排除，并列出需要人工确认的问题。\n\n")
	builder.WriteString("## 筛选条件\n\n")
	fmt.Fprintf(&builder, "- 股票池：%s\n", title)
	fmt.Fprintf(&builder, "- 开始日期：%s\n", valueOrDash(query.Get("start")))
	fmt.Fprintf(&builder, "- 结束日期：%s\n", valueOrDash(query.Get("end")))
	fmt.Fprintf(&builder, "- 最小成交额：%s\n", amountFilterLabel(query.Get("min_amount")))
	fmt.Fprintf(&builder, "- 最大成交额：%s\n", amountFilterLabel(query.Get("max_amount")))
	fmt.Fprintf(&builder, "- 只看标星：%s\n", yesNo(query.Get("starred") == "1"))
	fmt.Fprintf(&builder, "- 只看GPT星：%s\n", yesNo(query.Get("gpt_starred") == "1"))
	fmt.Fprintf(&builder, "- 当前导出数量：%d\n\n", len(rows))
	builder.WriteString("## 输出格式要求\n\n")
	builder.WriteString("请用表格输出：股票代码、名称、行业、入池原因、基本面摘要、主要风险、评级、是否建议GPT星标、需要人工确认的问题。\n")
	builder.WriteString("如果评级为优先研究，请在“是否建议GPT星标”列填“是”，否则填“否”。\n\n")
	builder.WriteString("## 股票列表\n\n")
	if len(rows) == 0 {
		builder.WriteString("当前筛选条件下没有股票。\n")
		return builder.String()
	}
	for index, row := range rows {
		fmt.Fprintf(&builder, "### %d. %s %s\n\n", index+1, row.StockCode, row.StockName)
		fmt.Fprintf(&builder, "- 股票池：%s\n", title)
		fmt.Fprintf(&builder, "- 行业：%s\n", valueOrDash(row.SectorName))
		fmt.Fprintf(&builder, "- 入池时间：%s\n", valueOrDash(row.GmtCreate))
		fmt.Fprintf(&builder, "- 当前GPT星：%s\n", yesNo(row.GPTStar == 1))
		fmt.Fprintf(&builder, "- 雪球链接：%s\n", xueqiuURL(row.StockCode))
		fmt.Fprintf(&builder, "- 收盘价：%.2f\n", row.ClosePrice)
		fmt.Fprintf(&builder, "- 最高价：%.2f\n", row.MaxPrice)
		fmt.Fprintf(&builder, "- 最低价：%.2f\n", row.MinPrice)
		fmt.Fprintf(&builder, "- %s：%.2f%%\n", riseLabel(pool), row.Rise)
		fmt.Fprintf(&builder, "- 成交额：%.2f 亿\n", row.Amount/100000000)
		fmt.Fprintf(&builder, "- %s：%.2f%s\n", metricLabel(pool), row.Vol, metricSuffix(pool))
		if pool == "breakout" {
			fmt.Fprintf(&builder, "- 前高价：%.2f\n", row.BeforeMaxPrice)
			fmt.Fprintf(&builder, "- 前高日成交量：%.0f\n", row.BeforeMaxVol)
			fmt.Fprintf(&builder, "- 前高日期：%s\n", dateOnlyString(row.BeforeMaxTime))
		}
		if pool == "shadow" {
			fmt.Fprintf(&builder, "- 首次覆盖价：%s\n", priceOrDash(row.FirstCoverPrice))
			fmt.Fprintf(&builder, "- 首次覆盖时间：%s\n", dateOnlyString(row.FirstCoverTime))
			fmt.Fprintf(&builder, "- 最新覆盖价：%s\n", priceOrDash(row.NowCoverPrice))
			fmt.Fprintf(&builder, "- 最新覆盖时间：%s\n", dateOnlyString(row.NowCoverTime))
		}
		fmt.Fprintf(&builder, "- 入池原因：%s\n\n", stockPoolReason(pool))
	}
	return builder.String()
}

func stockPoolTitle(pool string) string {
	switch pool {
	case "volume":
		return "放量股票池"
	case "shadow":
		return "上影线试盘池"
	case "breakout":
		return "突破股票池"
	default:
		return "股票池"
	}
}

func stockPoolReason(pool string) string {
	switch pool {
	case "volume":
		return "当日上涨，成交量超过前一日三倍，并且超过近十日均量两倍，成交额满足流动性要求。"
	case "shadow":
		return "当日上涨且收阳，盘中最高价涨幅超过阈值，最高价突破近二十二日收盘压力，同时上影线长度大于阳线实体。"
	case "breakout":
		return "近三日高点递增，最新收盘上涨并接近或小幅突破前高，成交量不明显超过前高日，且均线结构多头排列。"
	default:
		return "满足当前股票池筛选规则。"
	}
}

func riseLabel(pool string) string {
	if pool == "shadow" {
		return "收盘涨幅"
	}
	return "涨跌幅"
}

func metricLabel(pool string) string {
	switch pool {
	case "volume":
		return "量比"
	case "shadow":
		return "最高价涨幅"
	case "breakout":
		return "量能接近度"
	default:
		return "指标"
	}
}

func metricSuffix(pool string) string {
	if pool == "shadow" {
		return "%"
	}
	return ""
}

func valueOrDash(value string) string {
	if strings.TrimSpace(value) == "" {
		return "-"
	}
	return value
}

func yesNo(value bool) string {
	if value {
		return "是"
	}
	return "否"
}

func amountFilterLabel(value string) string {
	if strings.TrimSpace(value) == "" {
		return "不限"
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return value
	}
	return fmt.Sprintf("%.2f 亿", parsed/100000000)
}

func priceOrDash(value float64) string {
	if value <= 0 {
		return "-"
	}
	return fmt.Sprintf("%.2f", value)
}

func dateOnlyString(value string) string {
	if strings.TrimSpace(value) == "" {
		return "-"
	}
	if len(value) >= 10 {
		return value[:10]
	}
	return value
}

func xueqiuURL(stockCode string) string {
	prefix := "SZ"
	if strings.HasPrefix(stockCode, "6") {
		prefix = "SH"
	}
	return fmt.Sprintf("https://xueqiu.com/S/%s%s", prefix, stockCode)
}

func isStockPoolTable(table string) bool {
	return table == "volume_stock" || table == "shadow_stock" || table == "breakout_stock"
}

func stockPoolTableByName(pool string) (string, error) {
	switch pool {
	case "volume":
		return "volume_stock", nil
	case "shadow":
		return "shadow_stock", nil
	case "breakout":
		return "breakout_stock", nil
	default:
		return "", fmt.Errorf("invalid stock pool")
	}
}

func DeleteStockPoolRows(ctx context.Context, db *sql.DB, table string, ids []int64) (int64, error) {
	if !isStockPoolTable(table) {
		return 0, fmt.Errorf("invalid stock pool table")
	}
	normalized, err := normalizeIDs(ids)
	if err != nil {
		return 0, err
	}
	args := idsToArgs(normalized)

	placeholders := strings.TrimRight(strings.Repeat("?,", len(args)), ",")
	sqlText := fmt.Sprintf("DELETE FROM %s WHERE id IN (%s)", table, placeholders)
	result, err := db.ExecContext(ctx, sqlText, args...)
	if err != nil {
		return 0, fmt.Errorf("delete %s rows failed: %w", table, err)
	}
	deleted, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("read deleted rows failed: %w", err)
	}
	if table == "shadow_stock" || table == "breakout_stock" {
		sourcePool, err := watchlistSourcePool(table)
		if err != nil {
			return 0, err
		}
		for _, arg := range args {
			id, ok := arg.(int64)
			if !ok {
				continue
			}
			if err := deleteWatchlistBySource(ctx, db, sourcePool, id); err != nil {
				return 0, err
			}
		}
	}
	return deleted, nil
}

func DeleteWatchlistRows(ctx context.Context, db *sql.DB, ids []int64) (int64, error) {
	if err := ensureWatchlistStockTable(ctx, db); err != nil {
		return 0, err
	}
	normalized, err := normalizeIDs(ids)
	if err != nil {
		return 0, err
	}
	args := idsToArgs(normalized)
	placeholders := strings.TrimRight(strings.Repeat("?,", len(args)), ",")
	result, err := db.ExecContext(ctx, fmt.Sprintf("DELETE FROM watchlist_stock WHERE id IN (%s)", placeholders), args...)
	if err != nil {
		return 0, fmt.Errorf("delete watchlist_stock rows failed: %w", err)
	}
	deleted, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("read deleted rows failed: %w", err)
	}
	return deleted, nil
}

func normalizeIDs(ids []int64) ([]int64, error) {
	if len(ids) == 0 {
		return nil, fmt.Errorf("ids cannot be empty")
	}
	if len(ids) > 500 {
		return nil, fmt.Errorf("operate at most 500 rows once")
	}
	seen := make(map[int64]struct{}, len(ids))
	result := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id <= 0 {
			return nil, fmt.Errorf("invalid id")
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		result = append(result, id)
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("ids cannot be empty")
	}
	return result, nil
}

func idsToArgs(ids []int64) []any {
	args := make([]any, 0, len(ids))
	for _, id := range ids {
		args = append(args, id)
	}
	return args
}

func UpdateStockPoolStart(ctx context.Context, db *sql.DB, table string, id int64, start int) error {
	if !isStockPoolTable(table) {
		return fmt.Errorf("invalid stock pool table")
	}
	if err := ensureStockPoolTable(ctx, db, table); err != nil {
		return err
	}
	if id <= 0 {
		return fmt.Errorf("invalid id")
	}
	if start != 0 {
		start = 1
	}
	sqlText := fmt.Sprintf("UPDATE %s SET `start` = ? WHERE id = ?", table)
	result, err := db.ExecContext(ctx, sqlText, start, id)
	if err != nil {
		return fmt.Errorf("update %s start failed: %w", table, err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read updated rows failed: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("row not found")
	}
	if table == "shadow_stock" || table == "breakout_stock" {
		if err := SyncWatchlistFromPoolRow(ctx, db, table, id); err != nil {
			return err
		}
	}
	return nil
}

func UpdateStockPoolGPTStar(ctx context.Context, db *sql.DB, table string, id int64, gptStar int) error {
	if !isStockPoolTable(table) {
		return fmt.Errorf("invalid stock pool table")
	}
	if err := ensureStockPoolTable(ctx, db, table); err != nil {
		return err
	}
	if id <= 0 {
		return fmt.Errorf("invalid id")
	}
	if gptStar != 0 {
		gptStar = 1
	}
	sqlText := fmt.Sprintf("UPDATE %s SET gpt_star = ? WHERE id = ?", table)
	result, err := db.ExecContext(ctx, sqlText, gptStar, id)
	if err != nil {
		return fmt.Errorf("update %s gpt_star failed: %w", table, err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read updated rows failed: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("row not found")
	}
	if table == "shadow_stock" || table == "breakout_stock" {
		if err := SyncWatchlistFromPoolRow(ctx, db, table, id); err != nil {
			return err
		}
	}
	return nil
}

func SyncWatchlistCandidates(ctx context.Context, db *sql.DB) (WatchlistRunResult, error) {
	if err := ensureWatchlistStockTable(ctx, db); err != nil {
		return WatchlistRunResult{}, err
	}
	result := WatchlistRunResult{}
	for _, table := range []string{"shadow_stock", "breakout_stock"} {
		ids, err := loadWatchlistCandidateIDs(ctx, db, table)
		if err != nil {
			return result, err
		}
		for _, id := range ids {
			if err := SyncWatchlistFromPoolRow(ctx, db, table, id); err != nil {
				result.Failed++
				log.Printf("[watchlist] sync candidate failed table=%s id=%d err=%v", table, id, err)
				continue
			}
			result.Synced++
		}
	}
	return result, nil
}

func loadWatchlistCandidateIDs(ctx context.Context, db *sql.DB, table string) ([]int64, error) {
	if err := ensureStockPoolTable(ctx, db, table); err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx, fmt.Sprintf("SELECT id FROM %s WHERE COALESCE(`start`, 0) = 1 AND COALESCE(gpt_star, 0) = 1 ORDER BY id", table))
	if err != nil {
		return nil, fmt.Errorf("load %s watchlist candidate ids failed: %w", table, err)
	}
	defer rows.Close()
	ids := make([]int64, 0)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan %s watchlist candidate id failed: %w", table, err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate %s watchlist candidate ids failed: %w", table, err)
	}
	return ids, nil
}

func SyncWatchlistFromPoolRow(ctx context.Context, db *sql.DB, table string, id int64) error {
	sourcePool, err := watchlistSourcePool(table)
	if err != nil {
		return err
	}
	if err := ensureWatchlistStockTable(ctx, db); err != nil {
		return err
	}
	if err := ensureStockPoolTable(ctx, db, table); err != nil {
		return err
	}
	if id <= 0 {
		return fmt.Errorf("invalid id")
	}
	if table == "shadow_stock" {
		return syncShadowWatchlistRow(ctx, db, sourcePool, id)
	}
	return syncBreakoutWatchlistRow(ctx, db, sourcePool, id)
}

func syncShadowWatchlistRow(ctx context.Context, db *sql.DB, sourcePool string, id int64) error {
	var row struct {
		StockCode       string
		StockName       string
		SectorID        int64
		SectorName      sql.NullString
		Start           int
		GPTStar         int
		FirstCoverPrice sql.NullFloat64
		FirstCoverTime  sql.NullTime
		NowCoverPrice   sql.NullFloat64
		NowCoverTime    sql.NullTime
	}
	err := db.QueryRowContext(ctx, `
SELECT stock_code, stock_name, sector_id, sector_name, COALESCE(`+"`start`"+`, 0), COALESCE(gpt_star, 0),
       first_cover_price, first_cover_time, now_cover_price, now_cover_time
FROM shadow_stock
WHERE id = ?`, id).Scan(&row.StockCode, &row.StockName, &row.SectorID, &row.SectorName, &row.Start, &row.GPTStar, &row.FirstCoverPrice, &row.FirstCoverTime, &row.NowCoverPrice, &row.NowCoverTime)
	if errors.Is(err, sql.ErrNoRows) {
		return deleteWatchlistBySource(ctx, db, sourcePool, id)
	}
	if err != nil {
		return fmt.Errorf("load shadow_stock id=%d for watchlist failed: %w", id, err)
	}
	if row.Start != 1 || row.GPTStar != 1 || !row.FirstCoverPrice.Valid || !row.FirstCoverTime.Valid {
		return deleteWatchlistBySource(ctx, db, sourcePool, id)
	}
	return upsertWatchlistRow(ctx, db, sourcePool, id, row.StockCode, row.StockName, row.SectorID, row.SectorName, row.FirstCoverTime.Time, row.FirstCoverPrice.Float64)
}

func syncBreakoutWatchlistRow(ctx context.Context, db *sql.DB, sourcePool string, id int64) error {
	var row struct {
		StockCode  string
		StockName  string
		SectorID   int64
		SectorName sql.NullString
		ClosePrice float64
		Start      int
		GPTStar    int
		GmtCreate  time.Time
	}
	err := db.QueryRowContext(ctx, `
SELECT stock_code, stock_name, sector_id, sector_name, COALESCE(close_price, 0), COALESCE(`+"`start`"+`, 0), COALESCE(gpt_star, 0), gmt_create
FROM breakout_stock
WHERE id = ?`, id).Scan(&row.StockCode, &row.StockName, &row.SectorID, &row.SectorName, &row.ClosePrice, &row.Start, &row.GPTStar, &row.GmtCreate)
	if errors.Is(err, sql.ErrNoRows) {
		return deleteWatchlistBySource(ctx, db, sourcePool, id)
	}
	if err != nil {
		return fmt.Errorf("load breakout_stock id=%d for watchlist failed: %w", id, err)
	}
	if row.Start != 1 || row.GPTStar != 1 || row.ClosePrice <= 0 {
		return deleteWatchlistBySource(ctx, db, sourcePool, id)
	}
	return upsertWatchlistRow(ctx, db, sourcePool, id, row.StockCode, row.StockName, row.SectorID, row.SectorName, row.GmtCreate, row.ClosePrice)
}

func upsertWatchlistRow(ctx context.Context, db *sql.DB, sourcePool string, sourceID int64, stockCode string, stockName string, sectorID int64, sectorName sql.NullString, joinTime time.Time, joinPrice float64) error {
	_, err := db.ExecContext(ctx, `
INSERT INTO watchlist_stock
  (source_pool, source_id, stock_code, stock_name, sector_id, sector_name, join_time, join_price)
VALUES
  (?, ?, ?, ?, ?, ?, ?, ?)
ON DUPLICATE KEY UPDATE
  stock_code = VALUES(stock_code),
  stock_name = VALUES(stock_name),
  sector_id = VALUES(sector_id),
  sector_name = VALUES(sector_name),
  join_time = VALUES(join_time),
  join_price = VALUES(join_price)`,
		sourcePool, sourceID, stockCode, stockName, sectorID, sectorName, joinTime, joinPrice)
	if err != nil {
		return fmt.Errorf("upsert watchlist_stock source=%s id=%d failed: %w", sourcePool, sourceID, err)
	}
	return nil
}

func deleteWatchlistBySource(ctx context.Context, db *sql.DB, sourcePool string, sourceID int64) error {
	if err := ensureWatchlistStockTable(ctx, db); err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, "DELETE FROM watchlist_stock WHERE source_pool = ? AND source_id = ?", sourcePool, sourceID); err != nil {
		return fmt.Errorf("delete watchlist_stock source=%s id=%d failed: %w", sourcePool, sourceID, err)
	}
	return nil
}

func watchlistSourcePool(table string) (string, error) {
	switch table {
	case "shadow_stock":
		return "shadow", nil
	case "breakout_stock":
		return "breakout", nil
	default:
		return "", fmt.Errorf("watchlist only supports shadow_stock and breakout_stock")
	}
}

func LoadShadowCoverRows(ctx context.Context, db *sql.DB, ids []int64) ([]ShadowCoverRow, error) {
	args := []any{}
	where := "WHERE s.region IN ('SH', 'SZ')"
	if len(ids) > 0 {
		normalized, err := normalizeIDs(ids)
		if err != nil {
			return nil, err
		}
		args = idsToArgs(normalized)
		where += fmt.Sprintf(" AND ss.id IN (%s)", strings.TrimRight(strings.Repeat("?,", len(args)), ","))
	}
	rows, err := db.QueryContext(ctx, fmt.Sprintf(`
SELECT ss.id, ss.stock_code, ss.stock_name, COALESCE(s.region, ''),
       ss.sector_id, ss.sector_name, COALESCE(ss.max_price, 0),
       ss.gmt_create, ss.first_cover_price
FROM shadow_stock ss
JOIN stock s ON s.stock_code = ss.stock_code
%s
ORDER BY ss.id
`, where), args...)
	if err != nil {
		return nil, fmt.Errorf("load shadow cover rows failed: %w", err)
	}
	defer rows.Close()

	var result []ShadowCoverRow
	for rows.Next() {
		var row ShadowCoverRow
		if err := rows.Scan(
			&row.ID,
			&row.StockCode,
			&row.StockName,
			&row.Region,
			&row.SectorID,
			&row.SectorName,
			&row.MaxPrice,
			&row.GmtCreate,
			&row.FirstCoverPrice,
		); err != nil {
			return nil, fmt.Errorf("scan shadow cover row failed: %w", err)
		}
		if isSTStockName(row.StockName) {
			continue
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate shadow cover rows failed: %w", err)
	}
	return result, nil
}

func LoadWatchlistUpdateRows(ctx context.Context, db *sql.DB, ids []int64) ([]watchlistUpdateRow, error) {
	args := []any{}
	where := "WHERE s.region IN ('SH', 'SZ')"
	if len(ids) > 0 {
		normalized, err := normalizeIDs(ids)
		if err != nil {
			return nil, err
		}
		args = idsToArgs(normalized)
		where += fmt.Sprintf(" AND ws.id IN (%s)", strings.TrimRight(strings.Repeat("?,", len(args)), ","))
	}
	rows, err := db.QueryContext(ctx, fmt.Sprintf(`
SELECT ws.id, ws.stock_code, ws.stock_name, COALESCE(s.region, ''),
       ws.sector_id, ws.sector_name, COALESCE(ws.join_price, 0)
FROM watchlist_stock ws
JOIN stock s ON s.stock_code = ws.stock_code
%s
ORDER BY ws.id`, where), args...)
	if err != nil {
		return nil, fmt.Errorf("load watchlist rows failed: %w", err)
	}
	defer rows.Close()

	result := make([]watchlistUpdateRow, 0)
	for rows.Next() {
		var row watchlistUpdateRow
		if err := rows.Scan(&row.ID, &row.StockCode, &row.StockName, &row.Region, &row.SectorID, &row.SectorName, &row.JoinPrice); err != nil {
			return nil, fmt.Errorf("scan watchlist row failed: %w", err)
		}
		if isSTStockName(row.StockName) {
			continue
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate watchlist rows failed: %w", err)
	}
	return result, nil
}

func UpdateShadowFirstCover(ctx context.Context, db *sql.DB, id int64, firstPrice float64, firstCoverTime time.Time, nowPrice float64, nowCoverTime time.Time) error {
	_, err := db.ExecContext(ctx, `
UPDATE shadow_stock
SET first_cover_price = ?, first_cover_time = ?,
    now_cover_price = ?, now_cover_time = ?
WHERE id = ?
`, firstPrice, firstCoverTime, nowPrice, nowCoverTime, id)
	if err != nil {
		return fmt.Errorf("update shadow first cover id=%d failed: %w", id, err)
	}
	return nil
}

func UpdateShadowNowCover(ctx context.Context, db *sql.DB, id int64, price float64, coverTime time.Time) error {
	_, err := db.ExecContext(ctx, `
UPDATE shadow_stock
SET now_cover_price = ?, now_cover_time = ?
WHERE id = ?
`, price, coverTime, id)
	if err != nil {
		return fmt.Errorf("update shadow now cover id=%d failed: %w", id, err)
	}
	return nil
}

func UpdateWatchlistPrice(ctx context.Context, db *sql.DB, id int64, price float64, priceTime time.Time, rise float64) error {
	_, err := db.ExecContext(ctx, `
UPDATE watchlist_stock
SET current_price = ?, `+"`current_time`"+` = ?, rise = ?
WHERE id = ?`, price, priceTime, rise, id)
	if err != nil {
		return fmt.Errorf("update watchlist price id=%d failed: %w", id, err)
	}
	return nil
}

func queryWatchlistRows(ctx context.Context, db *sql.DB, req *http.Request) (WatchlistStockPage, error) {
	if err := ensureWatchlistStockTable(ctx, db); err != nil {
		return WatchlistStockPage{}, err
	}
	query := req.URL.Query()
	startDate := query.Get("start")
	endDate := query.Get("end")
	sourcePool := query.Get("source_pool")
	sortField := allowedWatchlistSortField(query.Get("sort"))
	sortDir := "DESC"
	if query.Get("dir") == "asc" {
		sortDir = "ASC"
	}
	page := 1
	if value := query.Get("page"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed <= 0 {
			return WatchlistStockPage{}, fmt.Errorf("invalid page")
		}
		page = parsed
	}
	pageSize := 50
	if value := query.Get("page_size"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || (parsed != 10 && parsed != 50 && parsed != 100) {
			return WatchlistStockPage{}, fmt.Errorf("invalid page_size")
		}
		pageSize = parsed
	}
	args := []any{}
	where := "WHERE 1=1"
	if startDate != "" {
		where += " AND join_time >= ?"
		args = append(args, startDate+" 00:00:00")
	}
	if endDate != "" {
		where += " AND join_time <= ?"
		args = append(args, endDate+" 23:59:59")
	}
	if sourcePool != "" {
		if sourcePool != "shadow" && sourcePool != "breakout" {
			return WatchlistStockPage{}, fmt.Errorf("invalid source_pool")
		}
		where += " AND source_pool = ?"
		args = append(args, sourcePool)
	}

	var total int
	if err := db.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM watchlist_stock %s", where), args...).Scan(&total); err != nil {
		return WatchlistStockPage{}, fmt.Errorf("count watchlist_stock failed: %w", err)
	}
	offset := (page - 1) * pageSize
	args = append(args, pageSize, offset)
	rows, err := db.QueryContext(ctx, fmt.Sprintf(`
SELECT id, source_pool, source_id, stock_code, stock_name, sector_id, COALESCE(sector_name, ''),
       DATE_FORMAT(join_time, '%%Y-%%m-%%d %%H:%%i:%%s'), COALESCE(join_price, 0),
       COALESCE(current_price, 0), COALESCE(DATE_FORMAT(`+"`current_time`"+`, '%%Y-%%m-%%d %%H:%%i:%%s'), ''),
       COALESCE(rise, 0), DATE_FORMAT(gmt_create, '%%Y-%%m-%%d %%H:%%i:%%s')
FROM watchlist_stock
%s
ORDER BY %s %s
LIMIT ? OFFSET ?`, where, sortField, sortDir), args...)
	if err != nil {
		return WatchlistStockPage{}, fmt.Errorf("query watchlist_stock failed: %w", err)
	}
	defer rows.Close()

	result := make([]WatchlistStockRow, 0, pageSize)
	for rows.Next() {
		var row WatchlistStockRow
		if err := rows.Scan(
			&row.ID,
			&row.SourcePool,
			&row.SourceID,
			&row.StockCode,
			&row.StockName,
			&row.SectorID,
			&row.SectorName,
			&row.JoinTime,
			&row.JoinPrice,
			&row.CurrentPrice,
			&row.CurrentTime,
			&row.Rise,
			&row.GmtCreate,
		); err != nil {
			return WatchlistStockPage{}, fmt.Errorf("scan watchlist_stock failed: %w", err)
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return WatchlistStockPage{}, fmt.Errorf("iterate watchlist_stock failed: %w", err)
	}
	return WatchlistStockPage{Rows: result, Total: total, Page: page, PageSize: pageSize}, nil
}

func allowedWatchlistSortField(field string) string {
	switch field {
	case "source_pool":
		return "source_pool"
	case "stock_code":
		return "stock_code"
	case "stock_name":
		return "stock_name"
	case "sector_name":
		return "sector_name"
	case "join_time":
		return "join_time"
	case "join_price":
		return "join_price"
	case "current_price":
		return "current_price"
	case "current_time":
		return "`current_time`"
	case "rise":
		return "rise"
	default:
		return "join_time"
	}
}

func queryStockPoolRows(ctx context.Context, db *sql.DB, req *http.Request, table string) (StockPoolPage, error) {
	if !isStockPoolTable(table) {
		return StockPoolPage{}, fmt.Errorf("invalid stock pool table")
	}
	if err := ensureStockPoolTable(ctx, db, table); err != nil {
		return StockPoolPage{}, err
	}
	query := req.URL.Query()
	startDate := query.Get("start")
	endDate := query.Get("end")
	minAmount := query.Get("min_amount")
	maxAmount := query.Get("max_amount")
	coverBelow := query.Get("cover_below")
	starred := query.Get("starred")
	gptStarred := query.Get("gpt_starred")
	sortField := allowedSortField(table, query.Get("sort"))
	sortDir := "DESC"
	if query.Get("dir") == "asc" {
		sortDir = "ASC"
	}
	page := 1
	if value := query.Get("page"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed <= 0 {
			return StockPoolPage{}, fmt.Errorf("invalid page")
		}
		page = parsed
	}
	pageSize := 50
	if value := query.Get("page_size"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || (parsed != 10 && parsed != 50 && parsed != 100) {
			return StockPoolPage{}, fmt.Errorf("invalid page_size")
		}
		pageSize = parsed
	}

	args := []any{}
	where := "WHERE 1=1"
	if startDate != "" {
		where += " AND gmt_create >= ?"
		args = append(args, startDate+" 00:00:00")
	}
	if endDate != "" {
		where += " AND gmt_create <= ?"
		args = append(args, endDate+" 23:59:59")
	}
	if minAmount != "" {
		parsed, err := strconv.ParseFloat(minAmount, 64)
		if err != nil || parsed < 0 {
			return StockPoolPage{}, fmt.Errorf("invalid min_amount")
		}
		where += " AND amount >= ?"
		args = append(args, parsed)
	}
	if maxAmount != "" {
		parsed, err := strconv.ParseFloat(maxAmount, 64)
		if err != nil || parsed < 0 {
			return StockPoolPage{}, fmt.Errorf("invalid max_amount")
		}
		where += " AND amount <= ?"
		args = append(args, parsed)
	}
	if coverBelow == "1" {
		if table != "shadow_stock" {
			return StockPoolPage{}, fmt.Errorf("cover_below only supports shadow_stock")
		}
		where += " AND first_cover_price IS NOT NULL AND now_cover_price IS NOT NULL AND now_cover_price >= first_cover_price"
	}
	if starred == "1" {
		where += " AND COALESCE(`start`, 0) = 1"
	}
	if gptStarred == "1" {
		where += " AND COALESCE(gpt_star, 0) = 1"
	}
	countSQL := fmt.Sprintf("SELECT COUNT(*) FROM %s %s", table, where)
	countArgs := append([]any(nil), args...)
	var total int
	if err := db.QueryRowContext(ctx, countSQL, countArgs...).Scan(&total); err != nil {
		return StockPoolPage{}, fmt.Errorf("count %s failed: %w", table, err)
	}
	offset := (page - 1) * pageSize
	args = append(args, pageSize, offset)

	riseField, volField := rowMetricFields(table)
	coverFields := rowCoverFields(table)
	breakoutFields := rowBreakoutFields(table)
	sqlText := fmt.Sprintf(`
SELECT id, stock_code, stock_name, sector_id, COALESCE(sector_name, ''),
       COALESCE(close_price, 0), COALESCE(max_price, 0), COALESCE(min_price, 0),
       COALESCE(%s, 0), COALESCE(amount, 0), COALESCE(%s, 0), COALESCE(`+"`start`"+`, 0), COALESCE(gpt_star, 0),
       %s,
       %s,
       DATE_FORMAT(gmt_create, '%%Y-%%m-%%d %%H:%%i:%%s')
FROM %s
%s
ORDER BY %s %s
LIMIT ? OFFSET ?`, riseField, volField, breakoutFields, coverFields, table, where, sortField, sortDir)

	resultRows, err := db.QueryContext(ctx, sqlText, args...)
	if err != nil {
		return StockPoolPage{}, fmt.Errorf("query %s failed: %w", table, err)
	}
	defer resultRows.Close()

	var rows []VolumeStockRow
	for resultRows.Next() {
		var row VolumeStockRow
		if err := resultRows.Scan(
			&row.ID,
			&row.StockCode,
			&row.StockName,
			&row.SectorID,
			&row.SectorName,
			&row.ClosePrice,
			&row.MaxPrice,
			&row.MinPrice,
			&row.Rise,
			&row.Amount,
			&row.Vol,
			&row.Start,
			&row.GPTStar,
			&row.BeforeMaxPrice,
			&row.BeforeMaxVol,
			&row.BeforeMaxTime,
			&row.FirstCoverPrice,
			&row.FirstCoverTime,
			&row.NowCoverPrice,
			&row.NowCoverTime,
			&row.GmtCreate,
		); err != nil {
			return StockPoolPage{}, fmt.Errorf("scan %s failed: %w", table, err)
		}
		rows = append(rows, row)
	}
	if err := resultRows.Err(); err != nil {
		return StockPoolPage{}, fmt.Errorf("iterate %s failed: %w", table, err)
	}
	return StockPoolPage{Rows: rows, Total: total, Page: page, PageSize: pageSize}, nil
}

func rowCoverFields(table string) string {
	if table == "shadow_stock" {
		return "COALESCE(first_cover_price, 0), COALESCE(DATE_FORMAT(first_cover_time, '%Y-%m-%d %H:%i:%s'), ''), COALESCE(now_cover_price, 0), COALESCE(DATE_FORMAT(now_cover_time, '%Y-%m-%d %H:%i:%s'), '')"
	}
	return "0, '', 0, ''"
}

func rowBreakoutFields(table string) string {
	if table == "breakout_stock" {
		return "COALESCE(before_max_price, 0), COALESCE(before_max_vol, 0), COALESCE(DATE_FORMAT(before_max_time, '%Y-%m-%d %H:%i:%s'), '')"
	}
	return "0, 0, ''"
}

func rowMetricFields(table string) (string, string) {
	if table == "shadow_stock" {
		return "raise_rate", "high_rate"
	}
	return "rise", "vol"
}

func allowedSortField(table string, field string) string {
	switch field {
	case "stock_code":
		return "stock_code"
	case "stock_name":
		return "stock_name"
	case "sector_name":
		return "sector_name"
	case "close_price":
		return "close_price"
	case "max_price":
		return "max_price"
	case "min_price":
		return "min_price"
	case "rise":
		if table == "shadow_stock" {
			return "raise_rate"
		}
		return "rise"
	case "amount":
		return "amount"
	case "vol":
		if table == "shadow_stock" {
			return "high_rate"
		}
		return "vol"
	case "before_max_price":
		if table == "breakout_stock" {
			return "before_max_price"
		}
		return "gmt_create"
	case "before_max_vol":
		if table == "breakout_stock" {
			return "before_max_vol"
		}
		return "gmt_create"
	case "before_max_time":
		if table == "breakout_stock" {
			return "before_max_time"
		}
		return "gmt_create"
	case "start":
		return "`start`"
	case "gpt_star":
		return "gpt_star"
	case "gmt_create":
		return "gmt_create"
	default:
		return "gmt_create"
	}
}

func (r *VolumeRunner) scheduleDaily() {
	for {
		now := time.Now()
		next := nextWeekdayRun(now)
		wait := time.Until(next)
		if wait <= 0 {
			continue
		}
		log.Printf("[schedule] waiting next stock pool run at %s wait=%s", next.Format(time.RFC3339), wait.Round(time.Second))
		time.Sleep(wait)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
		result, err := r.RunAllDays(ctx, 1)
		if err != nil {
			log.Printf("[schedule] stock pool run failed: %v", err)
			cancel()
			continue
		}
		log.Printf("[schedule] stock pool run done: volume=%+v shadow=%+v breakout=%+v", result.Volume, result.Shadow, result.Breakout)

		cancel()

		ctx, cancel = context.WithTimeout(context.Background(), 2*time.Hour)
		coverResult, err := r.RunShadowCover(ctx)
		cancel()
		if err != nil {
			log.Printf("[schedule] shadow cover run failed: %v", err)
			continue
		}
		log.Printf("[schedule] shadow cover run done: %+v", coverResult)

		ctx, cancel = context.WithTimeout(context.Background(), 2*time.Hour)
		watchlistResult, err := r.RunWatchlist(ctx)
		cancel()
		if err != nil {
			log.Printf("[schedule] watchlist run failed: %v", err)
			continue
		}
		log.Printf("[schedule] watchlist run done: %+v", watchlistResult)
	}
}

func (r *VolumeRunner) scheduleMacroDaily() {
	for {
		now := time.Now()
		next := nextMacroRun(now)
		wait := time.Until(next)
		if wait <= 0 {
			continue
		}
		log.Printf("[schedule] waiting next macro market run at %s wait=%s", next.Format(time.RFC3339), wait.Round(time.Second))
		time.Sleep(wait)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		result, err := r.RunMacroMarketDays(ctx, 1)
		cancel()
		if err != nil {
			log.Printf("[schedule] macro market run failed: %v", err)
			continue
		}
		log.Printf("[schedule] macro market run done: target_date=%s inserted=%d updated=%d failed=%d rows=%d", result.TargetDate, result.Inserted, result.Updated, result.Failed, len(result.Rows))
	}
}

func nextWeekdayRun(now time.Time) time.Time {
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		location = time.FixedZone("Asia/Shanghai", 8*60*60)
	}
	localNow := now.In(location)
	next := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 15, 10, 0, 0, location)
	for !next.After(localNow) || next.Weekday() == time.Saturday || next.Weekday() == time.Sunday {
		if !next.After(localNow) {
			next = next.Add(24 * time.Hour)
			continue
		}
		if next.Weekday() == time.Saturday || next.Weekday() == time.Sunday {
			next = next.Add(24 * time.Hour)
		}
	}
	return next
}

func nextMacroRun(now time.Time) time.Time {
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		location = time.FixedZone("Asia/Shanghai", 8*60*60)
	}
	localNow := now.In(location)
	next := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 6, 0, 0, 0, location)
	if !next.After(localNow) {
		next = next.Add(24 * time.Hour)
	}
	return next
}

func yahooPeriods(now time.Time, days int) (int64, int64) {
	period2 := now.Add(24 * time.Hour).Unix()
	requiredTradingDays := days + 66
	lookbackDays := requiredTradingDays*2 + 7
	if lookbackDays < 100 {
		lookbackDays = 100
	}
	period1 := now.AddDate(0, 0, -lookbackDays).Unix()
	return period1, period2
}

func shadowCoverPeriods(now time.Time) (int64, int64) {
	period2 := now.Add(24 * time.Hour).Unix()
	period1 := now.AddDate(0, 0, -66).Unix()
	return period1, period2
}

func ensureWatchlistStockTable(ctx context.Context, db *sql.DB) error {
	ddl := `
CREATE TABLE IF NOT EXISTS watchlist_stock (
  id BIGINT NOT NULL AUTO_INCREMENT COMMENT '主键ID',
  source_pool VARCHAR(20) NOT NULL COMMENT '来源池',
  source_id BIGINT NOT NULL COMMENT '来源记录ID',
  stock_code VARCHAR(20) NOT NULL COMMENT '股票代码',
  stock_name VARCHAR(100) NOT NULL COMMENT '股票名称',
  sector_id BIGINT NOT NULL COMMENT '行业ID',
  sector_name VARCHAR(100) DEFAULT NULL COMMENT '行业名称',
  join_time TIMESTAMP NOT NULL COMMENT '加入监控时间',
  join_price DECIMAL(18,4) NOT NULL COMMENT '加入监控价格',
  current_price DECIMAL(18,4) DEFAULT NULL COMMENT '监控实时价格',
  ` + "`current_time`" + ` TIMESTAMP NULL DEFAULT NULL COMMENT '实时价格日期',
  rise DECIMAL(10,4) DEFAULT NULL COMMENT '监控涨跌幅',
  gmt_create TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
  gmt_update TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP COMMENT '更新时间',
  PRIMARY KEY (id),
  UNIQUE KEY uk_source (source_pool, source_id),
  KEY idx_stock_code (stock_code),
  KEY idx_join_time (join_time)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='标星股票监控表'`
	if _, err := db.ExecContext(ctx, ddl); err != nil {
		return fmt.Errorf("ensure watchlist_stock table failed: %w", err)
	}
	return nil
}

func ensureVolumeStockTable(ctx context.Context, db *sql.DB) error {
	ddl := `
CREATE TABLE IF NOT EXISTS volume_stock (
  id BIGINT NOT NULL AUTO_INCREMENT COMMENT '主键ID',
  stock_code VARCHAR(20) NOT NULL COMMENT '股票代码',
  stock_name VARCHAR(100) NOT NULL COMMENT '股票名称',
  sector_id BIGINT NOT NULL COMMENT '行业ID',
  sector_name VARCHAR(100) DEFAULT NULL COMMENT '行业名称',
  close_price DECIMAL(18,4) DEFAULT NULL COMMENT '收盘价',
  max_price DECIMAL(18,4) DEFAULT NULL COMMENT '最高价',
  min_price DECIMAL(18,4) DEFAULT NULL COMMENT '最低价',
  rise DECIMAL(10,4) DEFAULT NULL COMMENT '涨跌幅',
  amount DECIMAL(50,4) DEFAULT NULL COMMENT '成交额',
  vol DECIMAL(10,4) DEFAULT NULL COMMENT '量比',
  ` + "`start`" + ` INT NOT NULL DEFAULT 0 COMMENT '是否标星',
  gpt_star INT NOT NULL DEFAULT 0 COMMENT 'GPT分析标星',
  gmt_create TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
  gmt_update TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP COMMENT '更新时间',
  PRIMARY KEY (id),
  KEY idx_stock_code (stock_code),
  KEY idx_sector_id (sector_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='量比股票日行情表'`
	if _, err := db.ExecContext(ctx, ddl); err != nil {
		return fmt.Errorf("ensure volume_stock table failed: %w", err)
	}
	if err := ensureStockPoolStartColumn(ctx, db, "volume_stock"); err != nil {
		return err
	}
	if err := ensureStockPoolGPTStarColumn(ctx, db, "volume_stock"); err != nil {
		return err
	}
	return nil
}

func ensureShadowStockTable(ctx context.Context, db *sql.DB) error {
	ddl := `
CREATE TABLE IF NOT EXISTS shadow_stock (
  id BIGINT NOT NULL AUTO_INCREMENT COMMENT '主键ID',
  stock_code VARCHAR(20) NOT NULL COMMENT '股票代码',
  stock_name VARCHAR(100) NOT NULL COMMENT '股票名称',
  sector_id BIGINT NOT NULL COMMENT '行业ID',
  sector_name VARCHAR(100) DEFAULT NULL COMMENT '行业名称',
  close_price DECIMAL(18,4) DEFAULT NULL COMMENT '收盘价',
  max_price DECIMAL(18,4) DEFAULT NULL COMMENT '最高价',
  min_price DECIMAL(18,4) DEFAULT NULL COMMENT '最低价',
  first_cover_price DECIMAL(18,4) DEFAULT NULL COMMENT '首次覆盖价格',
  first_cover_time TIMESTAMP NULL DEFAULT NULL COMMENT '首次覆盖时间',
  now_cover_price DECIMAL(18,4) DEFAULT NULL COMMENT '最新覆盖价格',
  now_cover_time TIMESTAMP NULL DEFAULT NULL COMMENT '最新覆盖时间',
  raise_rate DECIMAL(10,4) DEFAULT NULL COMMENT '收盘涨幅',
  amount DECIMAL(50,4) DEFAULT NULL COMMENT '成交额',
  high_rate DECIMAL(10,4) DEFAULT NULL COMMENT '最高价涨幅',
  ` + "`start`" + ` INT NOT NULL DEFAULT 0 COMMENT '是否标星',
  gpt_star INT NOT NULL DEFAULT 0 COMMENT 'GPT分析标星',
  gmt_create TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
  gmt_update TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP COMMENT '更新时间',
  PRIMARY KEY (id),
  KEY idx_stock_code (stock_code),
  KEY idx_sector_id (sector_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='上影线试盘股票表'`
	if _, err := db.ExecContext(ctx, ddl); err != nil {
		return fmt.Errorf("ensure shadow_stock table failed: %w", err)
	}
	if err := ensureStockPoolStartColumn(ctx, db, "shadow_stock"); err != nil {
		return err
	}
	if err := ensureStockPoolGPTStarColumn(ctx, db, "shadow_stock"); err != nil {
		return err
	}
	if err := ensureShadowStockExtraColumns(ctx, db); err != nil {
		return err
	}
	return nil
}

func ensureShadowStockExtraColumns(ctx context.Context, db *sql.DB) error {
	if err := ensureTableColumn(ctx, db, "shadow_stock", "high_rate", "ALTER TABLE shadow_stock ADD COLUMN high_rate DECIMAL(10,4) DEFAULT NULL COMMENT '最高价涨幅'"); err != nil {
		return err
	}
	hasShadowRate, err := tableColumnExists(ctx, db, "shadow_stock", "shadow_rate")
	if err != nil {
		return err
	}
	if hasShadowRate {
		if _, err := db.ExecContext(ctx, "UPDATE shadow_stock SET high_rate = shadow_rate WHERE high_rate IS NULL"); err != nil {
			return fmt.Errorf("copy shadow_stock.shadow_rate to high_rate failed: %w", err)
		}
	}
	return nil
}

func ensureBreakoutStockTable(ctx context.Context, db *sql.DB) error {
	ddl := `
CREATE TABLE IF NOT EXISTS breakout_stock (
  id BIGINT NOT NULL AUTO_INCREMENT COMMENT '主键ID',
  stock_code VARCHAR(20) NOT NULL COMMENT '股票代码',
  stock_name VARCHAR(100) NOT NULL COMMENT '股票名称',
  sector_id BIGINT NOT NULL COMMENT '行业ID',
  sector_name VARCHAR(100) DEFAULT NULL COMMENT '行业名称',
  close_price DECIMAL(18,4) DEFAULT NULL COMMENT '收盘价',
  max_price DECIMAL(18,4) DEFAULT NULL COMMENT '最高价',
  min_price DECIMAL(18,4) DEFAULT NULL COMMENT '最低价',
  before_max_price DECIMAL(18,4) DEFAULT NULL COMMENT '前高价',
  before_max_vol DECIMAL(20,4) DEFAULT NULL COMMENT '前高日成交量',
  before_max_time TIMESTAMP NULL DEFAULT NULL COMMENT '前高日期',
  rise DECIMAL(10,4) DEFAULT NULL COMMENT '涨跌幅',
  amount DECIMAL(50,4) DEFAULT NULL COMMENT '成交额',
  vol DECIMAL(10,4) DEFAULT NULL COMMENT '今日成交量/前高日成交量',
  ` + "`start`" + ` INT NOT NULL DEFAULT 0 COMMENT '是否标星',
  gpt_star INT NOT NULL DEFAULT 0 COMMENT 'GPT分析标星',
  gmt_create TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
  gmt_update TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP COMMENT '更新时间',
  PRIMARY KEY (id),
  KEY idx_stock_code (stock_code),
  KEY idx_sector_id (sector_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='突破股票池表'`
	if _, err := db.ExecContext(ctx, ddl); err != nil {
		return fmt.Errorf("ensure breakout_stock table failed: %w", err)
	}
	if err := ensureStockPoolStartColumn(ctx, db, "breakout_stock"); err != nil {
		return err
	}
	if err := ensureStockPoolGPTStarColumn(ctx, db, "breakout_stock"); err != nil {
		return err
	}
	if err := ensureBreakoutStockExtraColumns(ctx, db); err != nil {
		return err
	}
	return nil
}

func ensureBreakoutStockExtraColumns(ctx context.Context, db *sql.DB) error {
	if err := ensureTableColumn(ctx, db, "breakout_stock", "before_max_price", "ALTER TABLE breakout_stock ADD COLUMN before_max_price DECIMAL(18,4) DEFAULT NULL COMMENT '前高价'"); err != nil {
		return err
	}
	if err := ensureTableColumn(ctx, db, "breakout_stock", "before_max_vol", "ALTER TABLE breakout_stock ADD COLUMN before_max_vol DECIMAL(20,4) DEFAULT NULL COMMENT '前高日成交量'"); err != nil {
		return err
	}
	if err := ensureTableColumn(ctx, db, "breakout_stock", "before_max_time", "ALTER TABLE breakout_stock ADD COLUMN before_max_time TIMESTAMP NULL DEFAULT NULL COMMENT '前高日期'"); err != nil {
		return err
	}
	return nil
}

func ensureStockPoolTable(ctx context.Context, db *sql.DB, table string) error {
	switch table {
	case "volume_stock":
		return ensureVolumeStockTable(ctx, db)
	case "shadow_stock":
		return ensureShadowStockTable(ctx, db)
	case "breakout_stock":
		return ensureBreakoutStockTable(ctx, db)
	default:
		return fmt.Errorf("invalid stock pool table")
	}
}

func ensureStockPoolStartColumn(ctx context.Context, db *sql.DB, table string) error {
	if !isStockPoolTable(table) {
		return fmt.Errorf("invalid stock pool table")
	}
	return ensureTableColumn(ctx, db, table, "start", fmt.Sprintf("ALTER TABLE %s ADD COLUMN `start` INT NOT NULL DEFAULT 0 COMMENT '是否标星'", table))
}

func ensureStockPoolGPTStarColumn(ctx context.Context, db *sql.DB, table string) error {
	if !isStockPoolTable(table) {
		return fmt.Errorf("invalid stock pool table")
	}
	return ensureTableColumn(ctx, db, table, "gpt_star", fmt.Sprintf("ALTER TABLE %s ADD COLUMN gpt_star INT NOT NULL DEFAULT 0 COMMENT 'GPT分析标星'", table))
}

func ensureTableColumn(ctx context.Context, db *sql.DB, table string, column string, alterSQL string) error {
	exists, err := tableColumnExists(ctx, db, table, column)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	if _, err := db.ExecContext(ctx, alterSQL); err != nil {
		return fmt.Errorf("add %s.%s column failed: %w", table, column, err)
	}
	return nil
}

func tableColumnExists(ctx context.Context, db *sql.DB, table string, column string) (bool, error) {
	var exists int
	if err := db.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM information_schema.columns
WHERE table_schema = DATABASE()
  AND table_name = ?
  AND column_name = ?
`, table, column).Scan(&exists); err != nil {
		return false, fmt.Errorf("check %s.%s column failed: %w", table, column, err)
	}
	return exists > 0, nil
}

func InsertVolumeStock(ctx context.Context, db *sql.DB, stock VolumeStock) error {
	// No explicit transaction here: each Exec runs under MySQL autocommit,
	// so every matched stock is committed immediately.
	_, err := db.ExecContext(
		ctx,
		`
INSERT INTO volume_stock
  (stock_code, stock_name, sector_id, sector_name, close_price, max_price, min_price, rise, amount, vol, gmt_create)
VALUES
  (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`,
		stock.StockCode,
		stock.StockName,
		stock.SectorID,
		stock.SectorName,
		stock.ClosePrice,
		stock.MaxPrice,
		stock.MinPrice,
		stock.Rise,
		stock.Amount,
		stock.Vol,
		stock.GmtCreate,
	)
	if err != nil {
		return fmt.Errorf("insert volume_stock %s %s failed: %w", stock.StockCode, stock.StockName, err)
	}
	return nil
}

func InsertShadowStock(ctx context.Context, db *sql.DB, stock ShadowStock) error {
	_, err := db.ExecContext(
		ctx,
		`
INSERT INTO shadow_stock
  (stock_code, stock_name, sector_id, sector_name, close_price, max_price, min_price, raise_rate, amount, high_rate, gmt_create)
VALUES
  (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`,
		stock.StockCode,
		stock.StockName,
		stock.SectorID,
		stock.SectorName,
		stock.ClosePrice,
		stock.MaxPrice,
		stock.MinPrice,
		stock.Rise,
		stock.Amount,
		stock.Vol,
		stock.GmtCreate,
	)
	if err != nil {
		return fmt.Errorf("insert shadow_stock %s %s failed: %w", stock.StockCode, stock.StockName, err)
	}
	return nil
}

func InsertBreakoutStock(ctx context.Context, db *sql.DB, stock BreakoutStock) error {
	_, err := db.ExecContext(
		ctx,
		`
INSERT INTO breakout_stock
  (stock_code, stock_name, sector_id, sector_name, close_price, max_price, min_price, before_max_price, before_max_vol, before_max_time, rise, amount, vol, gmt_create)
VALUES
  (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`,
		stock.StockCode,
		stock.StockName,
		stock.SectorID,
		stock.SectorName,
		stock.ClosePrice,
		stock.MaxPrice,
		stock.MinPrice,
		stock.BeforeMaxPrice,
		stock.BeforeMaxVol,
		stock.BeforeMaxTime,
		stock.Rise,
		stock.Amount,
		stock.Vol,
		stock.GmtCreate,
	)
	if err != nil {
		return fmt.Errorf("insert breakout_stock %s %s failed: %w", stock.StockCode, stock.StockName, err)
	}
	return nil
}
