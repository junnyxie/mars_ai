package spider

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"html"
	"log"
	"math/rand"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"mars_ai/internal/config"
)

type Stock struct {
	StockCode  string
	StockName  string
	Region     sql.NullString
	SectorID   int64
	SectorName sql.NullString
}

type StockSyncResult struct {
	SectorCount int
	StockCount  int
}

type stockProgress struct {
	LastSuccessPage int
	Finished        bool
}

func SyncStocksByPage(ctx context.Context, db *sql.DB, cfg config.TonghuashunConfig, sectors []StockSector, sectorDBIDs map[int64]int64) (StockSyncResult, error) {
	if err := ensureStockCrawlProgressTable(ctx, db); err != nil {
		return StockSyncResult{}, err
	}

	client := &http.Client{
		Timeout: time.Duration(cfg.TimeoutSeconds) * time.Second,
	}

	result := StockSyncResult{SectorCount: len(sectors)}
	for _, sector := range sectors {
		if _, ok := sectorDBIDs[sector.SectorID]; !ok {
			log.Printf("[sync] skip sector without db id sector_id=%d sector_name=%s", sector.SectorID, sector.SectorName)
			continue
		}

		progress, err := loadStockProgress(ctx, db, sector)
		if err != nil {
			return result, err
		}
		if progress.Finished {
			log.Printf("[sync] skip finished sector_id=%d sector_name=%s last_success_page=%d", sector.SectorID, sector.SectorName, progress.LastSuccessPage)
			continue
		}

		pageSize := 0
		totalPages := 0
		startPage := progress.LastSuccessPage + 1
		if startPage < 1 {
			startPage = 1
		}
		log.Printf("[sync] start sector_id=%d sector_name=%s start_page=%d", sector.SectorID, sector.SectorName, startPage)

		for page := startPage; page <= cfg.StockMaxPages; page++ {
			if page > startPage {
				sleepBetweenRequests(cfg)
			}
			if totalPages > 0 && page > totalPages {
				if err := markStockProgress(ctx, db, sector, page-1, true, ""); err != nil {
					return result, err
				}
				log.Printf("[sync] reached last page sector_id=%d sector_name=%s total_pages=%d", sector.SectorID, sector.SectorName, totalPages)
				break
			}

			url := fmt.Sprintf(cfg.StockURLTemplate, sector.SectorID, page)
			referer := fmt.Sprintf("https://q.10jqka.com.cn/thshy/detail/code/%d/", sector.SectorID)
			htmlText, err := fetchTonghuashunHTMLWithRetry(ctx, client, cfg, url, referer)
			if err != nil {
				_ = markStockProgress(ctx, db, sector, page-1, false, err.Error())
				return result, fmt.Errorf("crawl sector stock failed sector_id=%d page=%d: %w", sector.SectorID, page, err)
			}

			stocks := parseStocksFromTable(htmlText, sector)
			if page == 1 {
				pageSize = len(stocks)
				totalPages = parseTotalPages(htmlText)
				if totalPages > 0 {
					log.Printf("[sync] detected total pages sector_id=%d sector_name=%s total_pages=%d", sector.SectorID, sector.SectorName, totalPages)
				}
			}
			if len(stocks) == 0 {
				if page == 1 && isVerificationPage(htmlText) {
					err := fmt.Errorf("tonghuashun returned verification page, check tonghuashun.v_js_path in config/config.json")
					_ = markStockProgress(ctx, db, sector, 0, false, err.Error())
					return result, err
				}
				if err := markStockProgress(ctx, db, sector, page-1, true, ""); err != nil {
					return result, err
				}
				log.Printf("[sync] stock page empty, mark finished sector_id=%d sector_name=%s page=%d", sector.SectorID, sector.SectorName, page)
				break
			}

			logStockPage(sector, page, stocks)
			if err := SaveStocks(ctx, db, stocks); err != nil {
				_ = markStockProgress(ctx, db, sector, page-1, false, err.Error())
				return result, err
			}
			result.StockCount += len(stocks)

			finished := false
			if totalPages > 0 && page >= totalPages {
				finished = true
			}
			if totalPages == 0 && pageSize > 0 && len(stocks) < pageSize {
				finished = true
				log.Printf(
					"[sync] detected short last page sector_id=%d sector_name=%s page=%d count=%d page_size=%d",
					sector.SectorID,
					sector.SectorName,
					page,
					len(stocks),
					pageSize,
				)
			}
			if err := markStockProgress(ctx, db, sector, page, finished, ""); err != nil {
				return result, err
			}
			log.Printf("[sync] saved page sector_id=%d sector_name=%s page=%d count=%d finished=%t", sector.SectorID, sector.SectorName, page, len(stocks), finished)
			if finished {
				break
			}
		}
		sleepBetweenRequests(cfg)
	}
	return result, nil
}

func CrawlStockData(ctx context.Context, cfg config.TonghuashunConfig, sectors []StockSector, sectorDBIDs map[int64]int64) ([]Stock, error) {
	client := &http.Client{
		Timeout: time.Duration(cfg.TimeoutSeconds) * time.Second,
	}

	stockMap := make(map[string]Stock)
	for _, sector := range sectors {
		if _, ok := sectorDBIDs[sector.SectorID]; !ok {
			log.Printf("[crawl] skip sector without db id sector_id=%d sector_name=%s", sector.SectorID, sector.SectorName)
			continue
		}

		pageSize := 0
		totalPages := 0
		for page := 1; page <= cfg.StockMaxPages; page++ {
			if page > 1 {
				sleepBetweenRequests(cfg)
			}
			if totalPages > 0 && page > totalPages {
				log.Printf("[crawl] reached last page sector_id=%d sector_name=%s total_pages=%d", sector.SectorID, sector.SectorName, totalPages)
				break
			}

			url := fmt.Sprintf(cfg.StockURLTemplate, sector.SectorID, page)
			referer := fmt.Sprintf("https://q.10jqka.com.cn/thshy/detail/code/%d/", sector.SectorID)
			htmlText, err := fetchTonghuashunHTMLWithRetry(ctx, client, cfg, url, referer)
			if err != nil {
				return nil, fmt.Errorf("crawl sector stock failed sector_id=%d page=%d: %w", sector.SectorID, page, err)
			}

			stocks := parseStocksFromTable(htmlText, sector)
			if page == 1 {
				pageSize = len(stocks)
				totalPages = parseTotalPages(htmlText)
				if totalPages > 0 {
					log.Printf("[crawl] detected total pages sector_id=%d sector_name=%s total_pages=%d", sector.SectorID, sector.SectorName, totalPages)
				}
			}
			if len(stocks) == 0 {
				if page == 1 && isVerificationPage(htmlText) {
					return nil, fmt.Errorf("tonghuashun returned verification page, check tonghuashun.v_js_path in config/config.json")
				}
				log.Printf("[crawl] stock page empty sector_id=%d sector_name=%s page=%d", sector.SectorID, sector.SectorName, page)
				break
			}

			for _, stock := range stocks {
				stockMap[stock.StockCode] = stock
			}
			logStockPage(sector, page, stocks)
			log.Printf(
				"[crawl] parsed stocks sector_id=%d sector_name=%s page=%d count=%d total=%d",
				sector.SectorID,
				sector.SectorName,
				page,
				len(stocks),
				len(stockMap),
			)
			if totalPages == 0 && pageSize > 0 && len(stocks) < pageSize {
				log.Printf(
					"[crawl] detected short last page sector_id=%d sector_name=%s page=%d count=%d page_size=%d",
					sector.SectorID,
					sector.SectorName,
					page,
					len(stocks),
					pageSize,
				)
				break
			}
		}
		sleepBetweenRequests(cfg)
	}

	stocks := make([]Stock, 0, len(stockMap))
	for _, stock := range stockMap {
		stocks = append(stocks, stock)
	}
	sort.Slice(stocks, func(i, j int) bool {
		return stocks[i].StockCode < stocks[j].StockCode
	})
	log.Printf("[crawl] parsed stocks total=%d", len(stocks))
	return stocks, nil
}

func logStockPage(sector StockSector, page int, stocks []Stock) {
	for index, stock := range stocks {
		log.Printf(
			"[crawl] stock item ths_sector_id=%d sector_name=%s page=%d index=%d stock_code=%s stock_name=%s region=%s stock_sector_id_value=%d stock_sector_name=%s",
			sector.SectorID,
			sector.SectorName,
			page,
			index+1,
			stock.StockCode,
			stock.StockName,
			stock.Region.String,
			stock.SectorID,
			stock.SectorName.String,
		)
	}
}

func parseStocksFromTable(htmlText string, sector StockSector) []Stock {
	rowPattern := regexp.MustCompile(`(?s)<tr>\s*<td>[^<]*</td>\s*<td>\s*<a[^>]*>\s*(\d{6})\s*</a>\s*</td>\s*<td>\s*<a[^>]*>\s*([^<]+)\s*</a>`)
	matches := rowPattern.FindAllStringSubmatch(htmlText, -1)

	stocks := make([]Stock, 0, len(matches))
	for _, match := range matches {
		if len(match) != 3 {
			continue
		}
		code := strings.TrimSpace(match[1])
		name := strings.TrimSpace(html.UnescapeString(match[2]))
		if code == "" || name == "" {
			continue
		}
		stocks = append(stocks, Stock{
			StockCode:  code,
			StockName:  name,
			Region:     nullString(marketPrefix(code)),
			SectorID:   sector.SectorID,
			SectorName: nullString(sector.SectorName),
		})
	}
	return stocks
}

func marketPrefix(stockCode string) string {
	switch {
	case strings.HasPrefix(stockCode, "600"),
		strings.HasPrefix(stockCode, "601"),
		strings.HasPrefix(stockCode, "603"),
		strings.HasPrefix(stockCode, "605"),
		strings.HasPrefix(stockCode, "688"),
		strings.HasPrefix(stockCode, "689"):
		return "SH"
	case strings.HasPrefix(stockCode, "000"),
		strings.HasPrefix(stockCode, "001"),
		strings.HasPrefix(stockCode, "002"),
		strings.HasPrefix(stockCode, "003"),
		strings.HasPrefix(stockCode, "300"),
		strings.HasPrefix(stockCode, "301"):
		return "SZ"
	case strings.HasPrefix(stockCode, "4"),
		strings.HasPrefix(stockCode, "8"),
		strings.HasPrefix(stockCode, "9"):
		return "BJ"
	default:
		return ""
	}
}

func parseTotalPages(htmlText string) int {
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`(?i)<span[^>]*class=["'][^"']*page_info[^"']*["'][^>]*>\s*\d+\s*/\s*(\d+)\s*</span>`),
		regexp.MustCompile(`(?i)page_info[^>]*>\s*\d+\s*/\s*(\d+)`),
		regexp.MustCompile(`(?i)共\s*(\d+)\s*页`),
	}
	for _, pattern := range patterns {
		matches := pattern.FindStringSubmatch(htmlText)
		if len(matches) != 2 {
			continue
		}
		total, err := strconv.Atoi(matches[1])
		if err == nil && total > 0 {
			return total
		}
	}
	return 0
}

func SaveStocks(ctx context.Context, db *sql.DB, stocks []Stock) error {
	for _, stock := range stocks {
		var id int64
		err := db.QueryRowContext(
			ctx,
			"SELECT id FROM stock WHERE stock_code = ? ORDER BY id LIMIT 1",
			stock.StockCode,
		).Scan(&id)

		switch {
		case err == nil:
			log.Printf("[db] update stock id=%d stock_code=%s stock_name=%s sector_id=%d sector_name=%s", id, stock.StockCode, stock.StockName, stock.SectorID, stock.SectorName.String)
			if _, err := db.ExecContext(
				ctx,
				"UPDATE stock SET stock_name = ?, region = ?, sector_id = ?, sector_name = ?, gmt_update = CURRENT_TIMESTAMP WHERE id = ?",
				stock.StockName,
				stock.Region,
				stock.SectorID,
				stock.SectorName,
				id,
			); err != nil {
				return fmt.Errorf("update stock %s %s failed: %w", stock.StockCode, stock.StockName, err)
			}
		case err == sql.ErrNoRows:
			log.Printf("[db] insert stock stock_code=%s stock_name=%s sector_id=%d sector_name=%s", stock.StockCode, stock.StockName, stock.SectorID, stock.SectorName.String)
			if _, err := db.ExecContext(
				ctx,
				"INSERT INTO stock (stock_code, stock_name, region, sector_id, sector_name) VALUES (?, ?, ?, ?, ?)",
				stock.StockCode,
				stock.StockName,
				stock.Region,
				stock.SectorID,
				stock.SectorName,
			); err != nil {
				return fmt.Errorf("insert stock %s %s failed: %w", stock.StockCode, stock.StockName, err)
			}
		default:
			return fmt.Errorf("query stock %s %s failed: %w", stock.StockCode, stock.StockName, err)
		}
	}
	log.Printf("[db] saved stock total=%d", len(stocks))
	return nil
}

func sleepBetweenRequests(cfg config.TonghuashunConfig) {
	if cfg.RequestSleepMaxMS <= 0 {
		return
	}
	sleepMS := cfg.RequestSleepMinMS
	if cfg.RequestSleepMaxMS > cfg.RequestSleepMinMS {
		sleepMS += rand.Intn(cfg.RequestSleepMaxMS - cfg.RequestSleepMinMS + 1)
	}
	log.Printf("[crawl] sleep ms=%d", sleepMS)
	time.Sleep(time.Duration(sleepMS) * time.Millisecond)
}

func fetchTonghuashunHTMLWithRetry(ctx context.Context, client *http.Client, cfg config.TonghuashunConfig, url string, referer string) (string, error) {
	var lastErr error
	for attempt := 1; attempt <= cfg.RetryCount+1; attempt++ {
		htmlText, err := fetchTonghuashunHTML(ctx, client, cfg, url, referer)
		if err == nil {
			return htmlText, nil
		}
		lastErr = err
		if !errors.Is(err, ErrTonghuashunBlocked) || attempt > cfg.RetryCount {
			break
		}

		sleepMS := cfg.RetrySleepMS * attempt
		log.Printf("[crawl] blocked retry attempt=%d/%d sleep_ms=%d err=%v", attempt, cfg.RetryCount, sleepMS, err)
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(time.Duration(sleepMS) * time.Millisecond):
		}
	}
	return "", lastErr
}

func ensureStockCrawlProgressTable(ctx context.Context, db *sql.DB) error {
	ddl := `
CREATE TABLE IF NOT EXISTS stock_crawl_progress (
  id BIGINT NOT NULL AUTO_INCREMENT COMMENT '主键ID',
  sector_id BIGINT NOT NULL COMMENT '同花顺行业代码',
  sector_name VARCHAR(100) NOT NULL COMMENT '行业名称',
  last_success_page INT NOT NULL DEFAULT 0 COMMENT '最后成功写入的页码',
  finished TINYINT(1) NOT NULL DEFAULT 0 COMMENT '是否已完成该行业',
  last_error TEXT NULL COMMENT '最后一次错误',
  gmt_create TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
  gmt_update TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP COMMENT '更新时间',
  PRIMARY KEY (id),
  UNIQUE KEY uk_sector_id (sector_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='股票爬取进度表'`
	if _, err := db.ExecContext(ctx, ddl); err != nil {
		return fmt.Errorf("ensure stock_crawl_progress table failed: %w", err)
	}
	return nil
}

func loadStockProgress(ctx context.Context, db *sql.DB, sector StockSector) (stockProgress, error) {
	var progress stockProgress
	var finished int
	err := db.QueryRowContext(
		ctx,
		"SELECT last_success_page, finished FROM stock_crawl_progress WHERE sector_id = ?",
		sector.SectorID,
	).Scan(&progress.LastSuccessPage, &finished)
	if err == sql.ErrNoRows {
		return stockProgress{}, nil
	}
	if err != nil {
		return stockProgress{}, fmt.Errorf("load stock crawl progress sector_id=%d failed: %w", sector.SectorID, err)
	}
	progress.Finished = finished == 1
	log.Printf("[sync] loaded progress sector_id=%d sector_name=%s last_success_page=%d finished=%t", sector.SectorID, sector.SectorName, progress.LastSuccessPage, progress.Finished)
	return progress, nil
}

func markStockProgress(ctx context.Context, db *sql.DB, sector StockSector, lastSuccessPage int, finished bool, lastError string) error {
	finishedValue := 0
	if finished {
		finishedValue = 1
	}
	_, err := db.ExecContext(
		ctx,
		`
INSERT INTO stock_crawl_progress (sector_id, sector_name, last_success_page, finished, last_error)
VALUES (?, ?, ?, ?, ?)
ON DUPLICATE KEY UPDATE
  sector_name = VALUES(sector_name),
  last_success_page = GREATEST(last_success_page, VALUES(last_success_page)),
  finished = VALUES(finished),
  last_error = VALUES(last_error),
  gmt_update = CURRENT_TIMESTAMP
`,
		sector.SectorID,
		sector.SectorName,
		lastSuccessPage,
		finishedValue,
		nullString(lastError),
	)
	if err != nil {
		return fmt.Errorf("mark stock crawl progress sector_id=%d page=%d failed: %w", sector.SectorID, lastSuccessPage, err)
	}
	log.Printf("[sync] progress sector_id=%d sector_name=%s last_success_page=%d finished=%t", sector.SectorID, sector.SectorName, lastSuccessPage, finished)
	return nil
}

func nullString(value string) sql.NullString {
	if value == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: value, Valid: true}
}
