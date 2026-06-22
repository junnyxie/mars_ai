package spider

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/text/encoding/simplifiedchinese"

	"mars_ai/internal/config"
)

var ErrTonghuashunBlocked = errors.New("tonghuashun blocked request")

type StockSector struct {
	ID         int64
	SectorID   int64
	SectorName string
}

func fetchTonghuashunHTML(ctx context.Context, client *http.Client, cfg config.TonghuashunConfig, url string, referer string) (string, error) {
	log.Printf("[crawl] request url=%s referer=%s", url, referer)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	request.Header.Set("User-Agent", cfg.UserAgent)
	request.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	request.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	request.Header.Set("Cache-Control", "no-cache")
	request.Header.Set("Connection", "keep-alive")
	request.Header.Set("Referer", referer)
	if strings.Contains(url, "/ajax/1/") {
		request.Header.Set("X-Requested-With", "XMLHttpRequest")
	}
	v, err := GenerateTonghuashunVCookie(cfg.VJSPath)
	if err != nil {
		return "", err
	}
	request.Header.Set("Cookie", fmt.Sprintf("v=%s", v))
	log.Printf("[crawl] generated tonghuashun v cookie length=%d", len(v))

	response, err := client.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	log.Printf("[crawl] response url=%s status=%s", url, response.Status)

	rawBody, err := io.ReadAll(response.Body)
	if err != nil {
		return "", err
	}
	bodyBytes, err := simplifiedchinese.GBK.NewDecoder().Bytes(rawBody)
	if err != nil {
		return "", err
	}
	htmlText := string(bodyBytes)

	if response.StatusCode == http.StatusUnauthorized {
		log.Printf("[crawl] unauthorized body preview=%s", previewText(htmlText, 300))
		return "", fmt.Errorf("%w: unauthorized, check tonghuashun.v_js_path in config/config.json", ErrTonghuashunBlocked)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		log.Printf("[crawl] unexpected response body preview=%s", previewText(htmlText, 300))
		return "", fmt.Errorf("tonghuashun response status: %s", response.Status)
	}

	return htmlText, nil
}

func previewText(text string, limit int) string {
	text = strings.Join(strings.Fields(text), " ")
	if len([]rune(text)) <= limit {
		return text
	}
	runes := []rune(text)
	return string(runes[:limit]) + "..."
}

func CrawlSectorData(ctx context.Context, cfg config.TonghuashunConfig) ([]StockSector, error) {
	client := &http.Client{
		Timeout: time.Duration(cfg.TimeoutSeconds) * time.Second,
	}

	htmlText, err := fetchTonghuashunHTML(ctx, client, cfg, cfg.SectorURL, "https://q.10jqka.com.cn/gn/")
	if err != nil {
		return nil, err
	}

	pattern := regexp.MustCompile(`<a\s+[^>]*href=["'][^"']*/thshy/detail/code/(\d+)/[^"']*["'][^>]*>([^<]+)</a>`)
	sectorMap := make(map[int64]string)
	for _, matches := range pattern.FindAllStringSubmatch(htmlText, -1) {
		if len(matches) != 3 {
			continue
		}
		sectorID, err := strconv.ParseInt(matches[1], 10, 64)
		if err != nil {
			continue
		}
		sectorName := strings.TrimSpace(html.UnescapeString(matches[2]))
		if sectorName == "" {
			continue
		}
		sectorMap[sectorID] = sectorName
	}
	if len(sectorMap) == 0 {
		if isVerificationPage(htmlText) {
			return nil, fmt.Errorf("tonghuashun returned verification page, check tonghuashun.v_js_path in config/config.json")
		}
		return nil, fmt.Errorf("no sector links found in tonghuashun page")
	}

	sectors := make([]StockSector, 0, len(sectorMap))
	for sectorID, sectorName := range sectorMap {
		sectors = append(sectors, StockSector{
			SectorID:   sectorID,
			SectorName: sectorName,
		})
	}
	sort.Slice(sectors, func(i, j int) bool {
		return sectors[i].SectorID < sectors[j].SectorID
	})
	log.Printf("[crawl] parsed sectors=%d", len(sectors))
	return sectors, nil
}

func isVerificationPage(html string) bool {
	hasSectorList := strings.Contains(html, "/thshy/detail/code/")
	if hasSectorList {
		return false
	}
	return strings.Contains(html, "chameleon") &&
		strings.Contains(html, "<body>") &&
		strings.Contains(html, "window.location.href")
}

func SaveStockSectors(ctx context.Context, db *sql.DB, sectors []StockSector) (map[int64]int64, error) {
	sectorDBIDs := make(map[int64]int64, len(sectors))
	for index := range sectors {
		sector := &sectors[index]
		var id int64
		err := db.QueryRowContext(
			ctx,
			"SELECT id FROM stock_sector WHERE sector_id = ? OR sector_name = ? ORDER BY id LIMIT 1",
			sector.SectorID,
			sector.SectorName,
		).Scan(&id)

		switch {
		case err == nil:
			log.Printf("[db] update stock_sector id=%d sector_id=%d sector_name=%s", id, sector.SectorID, sector.SectorName)
			if _, err := db.ExecContext(
				ctx,
				"UPDATE stock_sector SET sector_id = ?, sector_name = ?, gmt_update = CURRENT_TIMESTAMP WHERE id = ?",
				sector.SectorID,
				sector.SectorName,
				id,
			); err != nil {
				return nil, fmt.Errorf("update stock_sector %d %s failed: %w", sector.SectorID, sector.SectorName, err)
			}
		case err == sql.ErrNoRows:
			log.Printf("[db] insert stock_sector sector_id=%d sector_name=%s", sector.SectorID, sector.SectorName)
			result, err := db.ExecContext(
				ctx,
				"INSERT INTO stock_sector (sector_id, sector_name) VALUES (?, ?)",
				sector.SectorID,
				sector.SectorName,
			)
			if err != nil {
				return nil, fmt.Errorf("insert stock_sector %d %s failed: %w", sector.SectorID, sector.SectorName, err)
			}
			id, err = result.LastInsertId()
			if err != nil {
				return nil, fmt.Errorf("get stock_sector last insert id failed: %w", err)
			}
		default:
			return nil, fmt.Errorf("query stock_sector %d %s failed: %w", sector.SectorID, sector.SectorName, err)
		}
		sector.ID = id
		sectorDBIDs[sector.SectorID] = id
	}
	log.Printf("[db] saved stock_sector total=%d", len(sectors))
	return sectorDBIDs, nil
}

func LoadStockSectors(ctx context.Context, db *sql.DB) ([]StockSector, map[int64]int64, error) {
	rows, err := db.QueryContext(
		ctx,
		"SELECT id, sector_id, sector_name FROM stock_sector WHERE sector_id IS NOT NULL ORDER BY sector_id",
	)
	if err != nil {
		return nil, nil, fmt.Errorf("query stock_sector failed: %w", err)
	}
	defer rows.Close()

	var sectors []StockSector
	sectorDBIDs := make(map[int64]int64)
	for rows.Next() {
		var sector StockSector
		if err := rows.Scan(&sector.ID, &sector.SectorID, &sector.SectorName); err != nil {
			return nil, nil, fmt.Errorf("scan stock_sector failed: %w", err)
		}
		sectors = append(sectors, sector)
		sectorDBIDs[sector.SectorID] = sector.ID
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("iterate stock_sector failed: %w", err)
	}
	if len(sectors) == 0 {
		return nil, nil, fmt.Errorf("no stock_sector rows found")
	}
	log.Printf("[db] loaded stock_sector total=%d", len(sectors))
	return sectors, sectorDBIDs, nil
}
