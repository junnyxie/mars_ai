package yahoo

import (
	"context"
	"database/sql"
	"encoding/json"
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

type CombinedRunResult struct {
	Volume RunResult `json:"volume"`
	Shadow RunResult `json:"shadow"`
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

func NewVolumeRunner(db *sql.DB) *VolumeRunner {
	return &VolumeRunner{
		db:     db,
		client: &http.Client{Timeout: 20 * time.Second},
	}
}

func (r *VolumeRunner) Run(ctx context.Context) (RunResult, error) {
	return r.RunDays(ctx, 1)
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
			continue
		}
		r.insertVolumeStocks(ctx, volumes, &result)
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
			continue
		}
		r.insertShadowStocks(ctx, shadows, &result)
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

	stocks, err := LoadYahooSupportedStocks(ctx, r.db)
	if err != nil {
		return CombinedRunResult{}, err
	}

	period1, period2 := yahooPeriods(time.Now(), days)
	result := CombinedRunResult{
		Volume: RunResult{TotalStocks: len(stocks)},
		Shadow: RunResult{TotalStocks: len(stocks)},
	}
	for index, meta := range stocks {
		if index > 0 {
			time.Sleep(100 * time.Millisecond)
		}

		quote, timestamps, _, err := fetchDailyQuote(ctx, r.client, meta, period1, period2)
		if err != nil {
			result.Volume.Failed++
			result.Shadow.Failed++
			log.Printf("[stock-pool] fetch failed stock_code=%s stock_name=%s err=%v", meta.StockCode, meta.StockName, err)
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
	}
	return result, nil
}

func (r *VolumeRunner) RunShadowCover(ctx context.Context) (CoverRunResult, error) {
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
	rows, err := LoadShadowCoverRows(ctx, r.db)
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
		}()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "started"})
	})
	mux.HandleFunc("/api/volume-stocks", r.handleVolumeStocks)
	mux.HandleFunc("/api/shadow-stocks", r.handleShadowStocks)
	mux.HandleFunc("/api/volume-stocks/delete", r.handleDeleteVolumeStocks)
	mux.HandleFunc("/api/shadow-stocks/delete", r.handleDeleteShadowStocks)
	mux.HandleFunc("/api/volume-stocks/start", r.handleUpdateVolumeStart)
	mux.HandleFunc("/api/shadow-stocks/start", r.handleUpdateShadowStart)
	mux.Handle("/", http.FileServer(http.Dir("web")))

	go r.scheduleDaily()
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

func (r *VolumeRunner) handleDeleteVolumeStocks(w http.ResponseWriter, req *http.Request) {
	r.handleDeleteStockPoolRows(w, req, "volume_stock")
}

func (r *VolumeRunner) handleDeleteShadowStocks(w http.ResponseWriter, req *http.Request) {
	r.handleDeleteStockPoolRows(w, req, "shadow_stock")
}

func (r *VolumeRunner) handleUpdateVolumeStart(w http.ResponseWriter, req *http.Request) {
	r.handleUpdateStockPoolStart(w, req, "volume_stock")
}

func (r *VolumeRunner) handleUpdateShadowStart(w http.ResponseWriter, req *http.Request) {
	r.handleUpdateStockPoolStart(w, req, "shadow_stock")
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

func DeleteStockPoolRows(ctx context.Context, db *sql.DB, table string, ids []int64) (int64, error) {
	if table != "volume_stock" && table != "shadow_stock" {
		return 0, fmt.Errorf("invalid stock pool table")
	}
	if len(ids) == 0 {
		return 0, fmt.Errorf("ids cannot be empty")
	}
	if len(ids) > 500 {
		return 0, fmt.Errorf("delete at most 500 rows once")
	}

	seen := make(map[int64]struct{}, len(ids))
	args := make([]any, 0, len(ids))
	for _, id := range ids {
		if id <= 0 {
			return 0, fmt.Errorf("invalid id")
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		args = append(args, id)
	}
	if len(args) == 0 {
		return 0, fmt.Errorf("ids cannot be empty")
	}

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
	return deleted, nil
}

func UpdateStockPoolStart(ctx context.Context, db *sql.DB, table string, id int64, start int) error {
	if table != "volume_stock" && table != "shadow_stock" {
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
	return nil
}

func LoadShadowCoverRows(ctx context.Context, db *sql.DB) ([]ShadowCoverRow, error) {
	rows, err := db.QueryContext(ctx, `
SELECT ss.id, ss.stock_code, ss.stock_name, COALESCE(s.region, ''),
       ss.sector_id, ss.sector_name, COALESCE(ss.max_price, 0),
       ss.gmt_create, ss.first_cover_price
FROM shadow_stock ss
JOIN stock s ON s.stock_code = ss.stock_code
WHERE s.region IN ('SH', 'SZ')
ORDER BY ss.id
`)
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

func queryStockPoolRows(ctx context.Context, db *sql.DB, req *http.Request, table string) (StockPoolPage, error) {
	if table != "volume_stock" && table != "shadow_stock" {
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
	sqlText := fmt.Sprintf(`
SELECT id, stock_code, stock_name, sector_id, COALESCE(sector_name, ''),
       COALESCE(close_price, 0), COALESCE(max_price, 0), COALESCE(min_price, 0),
       COALESCE(%s, 0), COALESCE(amount, 0), COALESCE(%s, 0), COALESCE(`+"`start`"+`, 0),
       %s,
       DATE_FORMAT(gmt_create, '%%Y-%%m-%%d %%H:%%i:%%s')
FROM %s
%s
ORDER BY %s %s
LIMIT ? OFFSET ?`, riseField, volField, coverFields, table, where, sortField, sortDir)

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

func rowMetricFields(table string) (string, string) {
	if table == "shadow_stock" {
		return "raise_rate", "shadow_rate"
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
			return "shadow_rate"
		}
		return "vol"
	case "start":
		return "`start`"
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
		log.Printf("[schedule] next stock pool run at %s", next.Format(time.RFC3339))
		time.Sleep(time.Until(next))
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
		result, err := r.RunAllDays(ctx, 1)
		if err != nil {
			log.Printf("[schedule] stock pool run failed: %v", err)
			cancel()
			continue
		}
		log.Printf("[schedule] stock pool run done: volume=%+v shadow=%+v", result.Volume, result.Shadow)

		coverResult, err := r.RunShadowCover(ctx)
		cancel()
		if err != nil {
			log.Printf("[schedule] shadow cover run failed: %v", err)
			continue
		}
		log.Printf("[schedule] shadow cover run done: %+v", coverResult)
	}
}

func nextWeekdayRun(now time.Time) time.Time {
	next := time.Date(now.Year(), now.Month(), now.Day(), 16, 10, 0, 0, now.Location())
	for !next.After(now) || next.Weekday() == time.Saturday || next.Weekday() == time.Sunday {
		if !next.After(now) {
			next = next.Add(24 * time.Hour)
			continue
		}
		if next.Weekday() == time.Saturday || next.Weekday() == time.Sunday {
			next = next.Add(24 * time.Hour)
		}
	}
	return next
}

func yahooPeriods(now time.Time, days int) (int64, int64) {
	period2 := now.Add(24 * time.Hour).Unix()
	requiredTradingDays := days + 1
	lookbackDays := requiredTradingDays*2 + 7
	if lookbackDays < 30 {
		lookbackDays = 30
	}
	period1 := now.AddDate(0, 0, -lookbackDays).Unix()
	return period1, period2
}

func shadowCoverPeriods(now time.Time) (int64, int64) {
	period2 := now.Add(24 * time.Hour).Unix()
	period1 := now.AddDate(0, 0, -66).Unix()
	return period1, period2
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
  gmt_create TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
  gmt_update TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP COMMENT '更新时间',
  PRIMARY KEY (id),
  KEY idx_stock_code (stock_code),
  KEY idx_sector_id (sector_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='量比股票日行情表'`
	if _, err := db.ExecContext(ctx, ddl); err != nil {
		return fmt.Errorf("ensure volume_stock table failed: %w", err)
	}
	if err := ensureStockPoolStartColumn(ctx, db, "volume_stock"); err != nil {
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
  shadow_rate DECIMAL(10,4) DEFAULT NULL COMMENT '上影率',
  ` + "`start`" + ` INT NOT NULL DEFAULT 0 COMMENT '是否标星',
  gmt_create TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
  gmt_update TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP COMMENT '更新时间',
  PRIMARY KEY (id),
  KEY idx_stock_code (stock_code),
  KEY idx_sector_id (sector_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='上影线试盘股票表'`
	if _, err := db.ExecContext(ctx, ddl); err != nil {
		return fmt.Errorf("ensure shadow_stock table failed: %w", err)
	}
	if err := ensureStockPoolStartColumn(ctx, db, "shadow_stock"); err != nil {
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
	default:
		return fmt.Errorf("invalid stock pool table")
	}
}

func ensureStockPoolStartColumn(ctx context.Context, db *sql.DB, table string) error {
	if table != "volume_stock" && table != "shadow_stock" {
		return fmt.Errorf("invalid stock pool table")
	}
	var exists int
	if err := db.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM information_schema.columns
WHERE table_schema = DATABASE()
  AND table_name = ?
  AND column_name = 'start'
`, table).Scan(&exists); err != nil {
		return fmt.Errorf("check %s start column failed: %w", table, err)
	}
	if exists > 0 {
		return nil
	}
	sqlText := fmt.Sprintf("ALTER TABLE %s ADD COLUMN `start` INT NOT NULL DEFAULT 0 COMMENT '是否标星'", table)
	if _, err := db.ExecContext(ctx, sqlText); err != nil {
		return fmt.Errorf("add %s start column failed: %w", table, err)
	}
	return nil
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
  (stock_code, stock_name, sector_id, sector_name, close_price, max_price, min_price, raise_rate, amount, shadow_rate, gmt_create)
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
