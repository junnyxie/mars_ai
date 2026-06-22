package spider

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"mars_ai/internal/config"
)

func CheckTonghuashunVCookie(ctx context.Context, cfg config.TonghuashunConfig) error {
	client := &http.Client{
		Timeout: time.Duration(cfg.TimeoutSeconds) * time.Second,
	}

	sectorID := int64(881101)
	page := 1
	url := fmt.Sprintf(cfg.StockURLTemplate, sectorID, page)
	referer := fmt.Sprintf("https://q.10jqka.com.cn/thshy/detail/code/%d/", sectorID)
	htmlText, err := fetchTonghuashunHTML(ctx, client, cfg, url, referer)
	if err != nil {
		return err
	}

	stocks := parseStocksFromTable(htmlText, StockSector{SectorID: sectorID, SectorName: "种植业"})
	log.Printf("[check] parsed stock rows=%d", len(stocks))
	logStockPage(StockSector{SectorID: sectorID, SectorName: "种植业"}, page, stocks)
	if len(stocks) == 0 {
		return fmt.Errorf("request succeeded but no stock rows parsed")
	}
	return nil
}
