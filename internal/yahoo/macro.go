package yahoo

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

type MacroMarketItem struct {
	Symbol         string  `json:"symbol"`
	MarketName     string  `json:"market_name"`
	MarketType     string  `json:"market_type"`
	ClosePrice     float64 `json:"close_price"`
	MaxPrice       float64 `json:"max_price"`
	MinPrice       float64 `json:"min_price"`
	Rise           float64 `json:"rise"`
	Volume         float64 `json:"volume"`
	TradeDate      string  `json:"trade_date"`
	RiskScore      int     `json:"risk_score"`
	CommodityScore int     `json:"commodity_score"`
	LiquidityScore int     `json:"liquidity_score"`
	TotalScore     float64 `json:"total_score"`
	MarketLevel    int     `json:"market_level"`
	MarketStatus   string  `json:"market_status"`
	Summary        string  `json:"summary,omitempty"`
	Note           string  `json:"note,omitempty"`
	Error          string  `json:"error,omitempty"`
	tradeTime      time.Time
}

type MacroMarketPreview struct {
	Rule       string            `json:"rule"`
	TargetDate string            `json:"target_date"`
	Days       int               `json:"days"`
	Inserted   int               `json:"inserted"`
	Updated    int               `json:"updated"`
	Failed     int               `json:"failed"`
	Rows       []MacroMarketItem `json:"rows"`
}

type MacroMarketPage struct {
	Latest  MacroMarketSnapshot `json:"latest"`
	History []MacroMarketScore  `json:"history"`
}

type MacroMarketSnapshot struct {
	TradeDate      string            `json:"trade_date"`
	RiskScore      int               `json:"risk_score"`
	CommodityScore int               `json:"commodity_score"`
	LiquidityScore int               `json:"liquidity_score"`
	TotalScore     float64           `json:"total_score"`
	MarketLevel    int               `json:"market_level"`
	MarketStatus   string            `json:"market_status"`
	Summary        string            `json:"summary,omitempty"`
	Rows           []MacroMarketItem `json:"rows"`
}

type MacroMarketScore struct {
	TradeDate      string  `json:"trade_date"`
	RiskScore      int     `json:"risk_score"`
	CommodityScore int     `json:"commodity_score"`
	LiquidityScore int     `json:"liquidity_score"`
	TotalScore     float64 `json:"total_score"`
	MarketLevel    int     `json:"market_level"`
	MarketStatus   string  `json:"market_status"`
	Summary        string  `json:"summary,omitempty"`
}

type macroMarketSymbol struct {
	Symbol        string
	MarketName    string
	MarketType    string
	PriceScale    float64
	DateShiftDays int
	Note          string
}

var macroMarketSymbols = []macroMarketSymbol{
	{Symbol: "^GSPC", MarketName: "标普500", MarketType: "美股指数"},
	{Symbol: "^IXIC", MarketName: "纳斯达克综合指数", MarketType: "美股指数"},
	{Symbol: "^DJI", MarketName: "道琼斯工业指数", MarketType: "美股指数"},
	{Symbol: "^N225", MarketName: "日经225", MarketType: "日本指数", DateShiftDays: -1},
	{Symbol: "^KS11", MarketName: "KOSPI", MarketType: "韩国指数", DateShiftDays: -1},
	{Symbol: "GC=F", MarketName: "黄金期货", MarketType: "商品"},
	{Symbol: "HG=F", MarketName: "铜期货折算美元/吨", MarketType: "商品", PriceScale: 2204.62262185, Note: "Yahoo HG=F 是 COMEX 铜磅口径，这里折算为美元/吨观察口径"},
	{Symbol: "CL=F", MarketName: "WTI原油", MarketType: "商品"},
	{Symbol: "DX-Y.NYB", MarketName: "美元指数", MarketType: "美元指数"},
	{Symbol: "^TNX", MarketName: "10年美债收益率", MarketType: "美债收益率"},
}

func FetchMacroMarketPreview(ctx context.Context, client *http.Client, now time.Time) MacroMarketPreview {
	return FetchMacroMarketDays(ctx, client, now, 1)
}

func FetchMacroMarketDays(ctx context.Context, client *http.Client, now time.Time, days int) MacroMarketPreview {
	if days < 1 {
		days = 1
	}
	if days > 60 {
		days = 60
	}
	targetDate := macroTargetDate(now)
	result := MacroMarketPreview{
		Rule:       "Asia/Shanghai 06:00 后取前一天数据，06:00 前取前两天数据",
		TargetDate: targetDate.Format("2006-01-02"),
		Days:       days,
		Rows:       make([]MacroMarketItem, 0, len(macroMarketSymbols)),
	}
	for _, symbol := range macroMarketSymbols {
		items, err := fetchMacroMarketItems(ctx, client, symbol, targetDate, days)
		if err != nil {
			result.Failed++
			items = []MacroMarketItem{{
				Symbol:     symbol.Symbol,
				MarketName: symbol.MarketName,
				MarketType: symbol.MarketType,
				TradeDate:  targetDate.Format("2006-01-02"),
				Error:      err.Error(),
			}}
		}
		result.Rows = append(result.Rows, items...)
	}
	applyMacroScores(result.Rows)
	return result
}

func macroTargetDate(now time.Time) time.Time {
	location := marketLocation()
	localNow := now.In(location)
	target := localNow.AddDate(0, 0, -1)
	sixAM := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 6, 0, 0, 0, location)
	if localNow.Before(sixAM) {
		target = localNow.AddDate(0, 0, -2)
	}
	return dateOnly(target)
}

func fetchMacroMarketItem(ctx context.Context, client *http.Client, symbol macroMarketSymbol, targetDate time.Time) (MacroMarketItem, error) {
	items, err := fetchMacroMarketItems(ctx, client, symbol, targetDate, 1)
	if err != nil {
		return MacroMarketItem{}, err
	}
	if len(items) == 0 {
		return MacroMarketItem{}, fmt.Errorf("no macro quote found for %s", symbol.Symbol)
	}
	return items[0], nil
}

func fetchMacroMarketItems(ctx context.Context, client *http.Client, symbol macroMarketSymbol, targetDate time.Time, days int) ([]MacroMarketItem, error) {
	quote, timestamps, err := fetchMacroDailyQuote(ctx, client, symbol.Symbol)
	if err != nil {
		return nil, err
	}
	indexes := macroQuoteIndexes(symbol, timestamps, quote, targetDate, days)
	if len(indexes) == 0 {
		return nil, fmt.Errorf("no macro quote found before target_date=%s", targetDate.Format("2006-01-02"))
	}
	items := make([]MacroMarketItem, 0, len(indexes))
	for _, index := range indexes {
		item, err := buildMacroMarketItem(symbol, quote, timestamps, index)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

func buildMacroMarketItem(symbol macroMarketSymbol, quote dailyQuote, timestamps []int64, index int) (MacroMarketItem, error) {
	previousIndex := previousCloseIndex(quote.Close, index)
	if previousIndex < 0 {
		return MacroMarketItem{}, fmt.Errorf("no previous quote found for %s", symbol.Symbol)
	}

	scale := macroPriceScale(symbol)
	closePrice := *quote.Close[index] * scale
	previousClose := *quote.Close[previousIndex] * scale
	rise := 0.0
	if previousClose != 0 {
		rise = (closePrice - previousClose) / previousClose * 100
	}
	volume := 0.0
	if quote.Volume[index] != nil {
		volume = float64(*quote.Volume[index])
	}
	return MacroMarketItem{
		Symbol:     symbol.Symbol,
		MarketName: symbol.MarketName,
		MarketType: symbol.MarketType,
		ClosePrice: round(closePrice, 4),
		MaxPrice:   round(*quote.High[index]*scale, 4),
		MinPrice:   round(*quote.Low[index]*scale, 4),
		Rise:       round(rise, 4),
		Volume:     volume,
		TradeDate:  macroTradeDate(symbol, timestamps[index]).Format("2006-01-02"),
		Note:       symbol.Note,
		tradeTime:  macroTradeDate(symbol, timestamps[index]),
	}, nil
}

func macroPriceScale(symbol macroMarketSymbol) float64 {
	if symbol.PriceScale == 0 {
		return 1
	}
	return symbol.PriceScale
}

func applyMacroScores(rows []MacroMarketItem) {
	byDate := make(map[string][]int)
	for index, row := range rows {
		if row.Error != "" || row.TradeDate == "" {
			continue
		}
		byDate[row.TradeDate] = append(byDate[row.TradeDate], index)
	}
	for _, indexes := range byDate {
		score := calculateMacroScore(rows, indexes)
		for _, index := range indexes {
			rows[index].RiskScore = score.RiskScore
			rows[index].CommodityScore = score.CommodityScore
			rows[index].LiquidityScore = score.LiquidityScore
			rows[index].TotalScore = score.TotalScore
			rows[index].MarketLevel = score.MarketLevel
			rows[index].MarketStatus = score.MarketStatus
			rows[index].Summary = score.Summary
		}
	}
}

type macroScore struct {
	RiskScore      int
	CommodityScore int
	LiquidityScore int
	TotalScore     float64
	MarketLevel    int
	MarketStatus   string
	Summary        string
}

func calculateMacroScore(rows []MacroMarketItem, indexes []int) macroScore {
	riskAvg := averageRiseBySymbols(rows, indexes, map[string]struct{}{
		"^GSPC": {}, "^IXIC": {}, "^N225": {}, "^KS11": {},
	})
	commodityAvg := averageRiseBySymbols(rows, indexes, map[string]struct{}{
		"GC=F": {}, "HG=F": {}, "CL=F": {},
	})
	dollarRise, dollarOK := riseBySymbol(rows, indexes, "DX-Y.NYB")
	rateRise, rateOK := riseBySymbol(rows, indexes, "^TNX")
	riskScore := scoreRiskAverage(riskAvg)
	commodityScore := scoreCommodityAverage(commodityAvg)
	liquidityScore := scoreLiquidity(dollarRise, dollarOK, rateRise, rateOK)
	total := round(0.5*float64(riskScore)+0.3*float64(commodityScore)+0.2*float64(liquidityScore), 4)
	level, status := macroMarketStatus(total)
	summary := macroSummaryText(total, riskScore, commodityScore, liquidityScore, status)
	return macroScore{
		RiskScore:      riskScore,
		CommodityScore: commodityScore,
		LiquidityScore: liquidityScore,
		TotalScore:     total,
		MarketLevel:    level,
		MarketStatus:   status,
		Summary:        summary,
	}
}

func averageRiseBySymbols(rows []MacroMarketItem, indexes []int, symbols map[string]struct{}) float64 {
	total := 0.0
	count := 0
	for _, index := range indexes {
		if _, ok := symbols[rows[index].Symbol]; !ok {
			continue
		}
		total += rows[index].Rise
		count++
	}
	if count == 0 {
		return 0
	}
	return total / float64(count)
}

func riseBySymbol(rows []MacroMarketItem, indexes []int, symbol string) (float64, bool) {
	for _, index := range indexes {
		if rows[index].Symbol == symbol {
			return rows[index].Rise, true
		}
	}
	return 0, false
}

func scoreRiskAverage(value float64) int {
	switch {
	case value > 1.5:
		return 2
	case value >= 0.5:
		return 1
	case value > -0.5:
		return 0
	case value >= -1:
		return -1
	default:
		return -2
	}
}

func scoreCommodityAverage(value float64) int {
	switch {
	case value > 1:
		return 1
	case value < -1:
		return -1
	default:
		return 0
	}
}

func scoreLiquidity(dollarRise float64, dollarOK bool, rateRise float64, rateOK bool) int {
	if !dollarOK || !rateOK {
		return 0
	}
	dollarUp := dollarRise > 0.05
	dollarDown := dollarRise < -0.05
	rateUp := rateRise > 0.05
	rateDown := rateRise < -0.05
	switch {
	case dollarDown && rateDown:
		return 2
	case dollarUp && rateUp:
		return -2
	case dollarUp || rateUp:
		return -1
	case dollarDown || rateDown:
		return 1
	default:
		return 0
	}
}

func macroMarketStatus(total float64) (int, string) {
	switch {
	case total >= 1.2:
		return 2, "强风险偏好（牛市加速）"
	case total >= 0.4:
		return 1, "偏多"
	case total > -0.4:
		return 0, "震荡"
	case total > -1.2:
		return -1, "偏空"
	default:
		return -2, "风险释放（系统性下跌）"
	}
}

func macroSummaryText(total float64, riskScore int, commodityScore int, liquidityScore int, status string) string {
	return fmt.Sprintf("今日市场为%s，总分%.2f；风险资产分%d，商品分%d，流动性分%d。%s，%s，%s，整体处于%s状态。",
		status,
		total,
		riskScore,
		commodityScore,
		liquidityScore,
		macroRiskSummary(riskScore),
		macroCommoditySummary(commodityScore),
		macroLiquiditySummary(liquidityScore),
		status,
	)
}

func macroRiskSummary(score int) string {
	switch {
	case score >= 2:
		return "全球风险资产明显走强"
	case score == 1:
		return "全球风险资产偏强"
	case score == -1:
		return "全球风险资产偏弱"
	case score <= -2:
		return "全球风险资产明显走弱"
	default:
		return "全球风险资产整体震荡"
	}
}

func macroCommoditySummary(score int) string {
	switch {
	case score > 0:
		return "商品端整体走强"
	case score < 0:
		return "商品端整体走弱"
	default:
		return "商品端整体中性"
	}
}

func macroLiquiditySummary(score int) string {
	switch {
	case score >= 2:
		return "美元和美债收益率同步回落，流动性明显改善"
	case score == 1:
		return "美元或美债收益率回落，流动性边际改善"
	case score == -1:
		return "美元或美债收益率上行，流动性边际收紧"
	case score <= -2:
		return "美元和美债收益率同步上行，流动性明显收紧"
	default:
		return "美元和美债收益率变化不明显，流动性中性"
	}
}

func fetchMacroDailyQuote(ctx context.Context, client *http.Client, symbol string) (dailyQuote, []int64, error) {
	requestURL := fmt.Sprintf(
		"https://query1.finance.yahoo.com/v8/finance/chart/%s?range=6mo&interval=1d&includePrePost=true&events=div%%7Csplit%%7Cearn&lang=en-US&region=US&source=cosaic",
		url.PathEscape(symbol),
	)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return dailyQuote{}, nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")

	resp, err := client.Do(req)
	if err != nil {
		return dailyQuote{}, nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return dailyQuote{}, nil, fmt.Errorf("yahoo macro response status=%s", resp.Status)
	}

	var data chartResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return dailyQuote{}, nil, err
	}
	if len(data.Chart.Result) == 0 || len(data.Chart.Result[0].Indicators.Quote) == 0 {
		return dailyQuote{}, nil, fmt.Errorf("empty yahoo macro chart result for %s", symbol)
	}
	result := data.Chart.Result[0]
	quote := result.Indicators.Quote[0]
	return dailyQuote{
		Close:  quote.Close,
		High:   quote.High,
		Low:    quote.Low,
		Open:   quote.Open,
		Volume: quote.Volume,
	}, result.Timestamp, nil
}

func macroQuoteIndex(timestamps []int64, quote dailyQuote, targetDate time.Time) int {
	indexes := macroQuoteIndexes(macroMarketSymbol{}, timestamps, quote, targetDate, 1)
	if len(indexes) == 0 {
		return -1
	}
	return indexes[0]
}

func macroQuoteIndexes(symbol macroMarketSymbol, timestamps []int64, quote dailyQuote, targetDate time.Time, limit int) []int {
	indexes := make([]int, 0, limit)
	maxLen := min(len(timestamps), len(quote.Close), len(quote.High), len(quote.Low))
	for i := maxLen - 1; i >= 0 && len(indexes) < limit; i-- {
		if quote.Close[i] == nil || quote.High[i] == nil || quote.Low[i] == nil {
			continue
		}
		if !macroTradeDate(symbol, timestamps[i]).After(targetDate) {
			indexes = append(indexes, i)
		}
	}
	return indexes
}

func previousCloseIndex(closeValues []*float64, todayIndex int) int {
	if todayIndex > len(closeValues) {
		todayIndex = len(closeValues)
	}
	for i := todayIndex - 1; i >= 0; i-- {
		if closeValues[i] != nil {
			return i
		}
	}
	return -1
}

func macroTradeDate(symbol macroMarketSymbol, timestamp int64) time.Time {
	tradeDate := dateOnly(time.Unix(timestamp, 0))
	if symbol.DateShiftDays != 0 {
		tradeDate = tradeDate.AddDate(0, 0, symbol.DateShiftDays)
	}
	return tradeDate
}

func ensureMacroMarketDailyTable(ctx context.Context, db *sql.DB) error {
	ddl := `
CREATE TABLE IF NOT EXISTS macro_market_daily (
  id BIGINT NOT NULL AUTO_INCREMENT COMMENT '主键ID',
  symbol VARCHAR(50) NOT NULL COMMENT 'Yahoo代码',
  market_name VARCHAR(100) NOT NULL COMMENT '名称',
  market_type VARCHAR(50) NOT NULL COMMENT '类型',
  close_price DECIMAL(18,4) DEFAULT NULL COMMENT '收盘价',
  max_price DECIMAL(18,4) DEFAULT NULL COMMENT '最高价',
  min_price DECIMAL(18,4) DEFAULT NULL COMMENT '最低价',
  rise DECIMAL(10,4) DEFAULT NULL COMMENT '涨跌幅',
  volume DECIMAL(30,4) DEFAULT NULL COMMENT '成交量',
  trade_time TIMESTAMP NOT NULL COMMENT '交易日时间',
  risk_score INT DEFAULT NULL COMMENT '风险资产分',
  commodity_score INT DEFAULT NULL COMMENT '商品分',
  liquidity_score INT DEFAULT NULL COMMENT '流动性分',
  total_score DECIMAL(10,4) DEFAULT NULL COMMENT '宏观总分',
  market_level INT DEFAULT NULL COMMENT '市场强弱等级',
  market_status VARCHAR(100) DEFAULT NULL COMMENT '市场状态',
  summary VARCHAR(500) DEFAULT NULL COMMENT '宏观总结',
  note VARCHAR(255) DEFAULT NULL COMMENT '备注',
  gmt_create TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
  gmt_update TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP COMMENT '更新时间',
  PRIMARY KEY (id),
  UNIQUE KEY uk_symbol_trade_time (symbol, trade_time),
  KEY idx_trade_time (trade_time),
  KEY idx_market_type (market_type)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='全球宏观市场日线表'`
	if _, err := db.ExecContext(ctx, ddl); err != nil {
		return fmt.Errorf("ensure macro_market_daily table failed: %w", err)
	}
	if err := ensureTableColumn(ctx, db, "macro_market_daily", "risk_score", "ALTER TABLE macro_market_daily ADD COLUMN risk_score INT DEFAULT NULL COMMENT '风险资产分'"); err != nil {
		return err
	}
	if err := ensureTableColumn(ctx, db, "macro_market_daily", "commodity_score", "ALTER TABLE macro_market_daily ADD COLUMN commodity_score INT DEFAULT NULL COMMENT '商品分'"); err != nil {
		return err
	}
	if err := ensureTableColumn(ctx, db, "macro_market_daily", "liquidity_score", "ALTER TABLE macro_market_daily ADD COLUMN liquidity_score INT DEFAULT NULL COMMENT '流动性分'"); err != nil {
		return err
	}
	if err := ensureTableColumn(ctx, db, "macro_market_daily", "total_score", "ALTER TABLE macro_market_daily ADD COLUMN total_score DECIMAL(10,4) DEFAULT NULL COMMENT '宏观总分'"); err != nil {
		return err
	}
	if err := ensureTableColumn(ctx, db, "macro_market_daily", "market_level", "ALTER TABLE macro_market_daily ADD COLUMN market_level INT DEFAULT NULL COMMENT '市场强弱等级'"); err != nil {
		return err
	}
	if err := ensureTableColumn(ctx, db, "macro_market_daily", "market_status", "ALTER TABLE macro_market_daily ADD COLUMN market_status VARCHAR(100) DEFAULT NULL COMMENT '市场状态'"); err != nil {
		return err
	}
	if err := ensureTableColumn(ctx, db, "macro_market_daily", "summary", "ALTER TABLE macro_market_daily ADD COLUMN summary VARCHAR(500) DEFAULT NULL COMMENT '宏观总结'"); err != nil {
		return err
	}
	if err := backfillMacroMarketSummary(ctx, db); err != nil {
		return err
	}
	return nil
}

func backfillMacroMarketSummary(ctx context.Context, db *sql.DB) error {
	rows, err := db.QueryContext(ctx, `
SELECT DATE_FORMAT(trade_time, '%Y-%m-%d'),
       COALESCE(MAX(risk_score), 0),
       COALESCE(MAX(commodity_score), 0),
       COALESCE(MAX(liquidity_score), 0),
       COALESCE(MAX(total_score), 0),
       COALESCE(MAX(market_status), '')
FROM macro_market_daily
WHERE summary IS NULL OR summary = ''
GROUP BY DATE_FORMAT(trade_time, '%Y-%m-%d')`)
	if err != nil {
		return fmt.Errorf("query macro market summary backfill failed: %w", err)
	}
	defer rows.Close()

	type pendingSummary struct {
		tradeDate      string
		riskScore      int
		commodityScore int
		liquidityScore int
		totalScore     float64
		marketStatus   string
	}
	pending := make([]pendingSummary, 0)
	for rows.Next() {
		var item pendingSummary
		if err := rows.Scan(&item.tradeDate, &item.riskScore, &item.commodityScore, &item.liquidityScore, &item.totalScore, &item.marketStatus); err != nil {
			return fmt.Errorf("scan macro market summary backfill failed: %w", err)
		}
		pending = append(pending, item)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate macro market summary backfill failed: %w", err)
	}

	for _, item := range pending {
		status := item.marketStatus
		if status == "" {
			_, status = macroMarketStatus(item.totalScore)
		}
		summary := macroSummaryText(item.totalScore, item.riskScore, item.commodityScore, item.liquidityScore, status)
		if _, err := db.ExecContext(ctx, `
UPDATE macro_market_daily
SET summary = ?
WHERE trade_time >= ? AND trade_time <= ? AND (summary IS NULL OR summary = '')`,
			summary, item.tradeDate+" 00:00:00", item.tradeDate+" 23:59:59"); err != nil {
			return fmt.Errorf("update macro market summary backfill failed date=%s: %w", item.tradeDate, err)
		}
	}
	return nil
}

func insertMacroMarketItem(ctx context.Context, db *sql.DB, item MacroMarketItem) (bool, error) {
	result, err := db.ExecContext(ctx, `
INSERT INTO macro_market_daily
  (symbol, market_name, market_type, close_price, max_price, min_price, rise, volume, trade_time, risk_score, commodity_score, liquidity_score, total_score, market_level, market_status, summary, note)
VALUES
  (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON DUPLICATE KEY UPDATE
  market_name = VALUES(market_name),
  market_type = VALUES(market_type),
  close_price = VALUES(close_price),
  max_price = VALUES(max_price),
  min_price = VALUES(min_price),
  rise = VALUES(rise),
  volume = VALUES(volume),
  risk_score = VALUES(risk_score),
  commodity_score = VALUES(commodity_score),
  liquidity_score = VALUES(liquidity_score),
  total_score = VALUES(total_score),
  market_level = VALUES(market_level),
  market_status = VALUES(market_status),
  summary = VALUES(summary),
  note = VALUES(note)
`, item.Symbol, item.MarketName, item.MarketType, item.ClosePrice, item.MaxPrice, item.MinPrice, item.Rise, item.Volume, item.tradeTime, item.RiskScore, item.CommodityScore, item.LiquidityScore, item.TotalScore, item.MarketLevel, item.MarketStatus, item.Summary, item.Note)
	if err != nil {
		return false, fmt.Errorf("insert macro_market_daily %s %s failed: %w", item.Symbol, item.TradeDate, err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, nil
	}
	return affected == 1, nil
}

func queryMacroMarketPage(ctx context.Context, db *sql.DB, limit int) (MacroMarketPage, error) {
	if limit <= 0 || limit > 120 {
		limit = 60
	}
	if err := ensureMacroMarketDailyTable(ctx, db); err != nil {
		return MacroMarketPage{}, err
	}
	history, err := queryMacroMarketHistory(ctx, db, limit)
	if err != nil {
		return MacroMarketPage{}, err
	}
	page := MacroMarketPage{History: history}
	if len(history) == 0 {
		return page, nil
	}
	latestDate := history[0].TradeDate
	rows, err := queryMacroMarketRows(ctx, db, latestDate)
	if err != nil {
		return MacroMarketPage{}, err
	}
	page.Latest = MacroMarketSnapshot{
		TradeDate:      latestDate,
		RiskScore:      history[0].RiskScore,
		CommodityScore: history[0].CommodityScore,
		LiquidityScore: history[0].LiquidityScore,
		TotalScore:     history[0].TotalScore,
		MarketLevel:    history[0].MarketLevel,
		MarketStatus:   history[0].MarketStatus,
		Summary:        history[0].Summary,
		Rows:           rows,
	}
	return page, nil
}

func queryMacroMarketSnapshot(ctx context.Context, db *sql.DB, tradeDate string) (MacroMarketSnapshot, error) {
	if tradeDate == "" {
		return MacroMarketSnapshot{}, fmt.Errorf("date is required")
	}
	if _, err := time.Parse("2006-01-02", tradeDate); err != nil {
		return MacroMarketSnapshot{}, fmt.Errorf("date must be YYYY-MM-DD")
	}
	if err := ensureMacroMarketDailyTable(ctx, db); err != nil {
		return MacroMarketSnapshot{}, err
	}
	rows, err := queryMacroMarketRows(ctx, db, tradeDate)
	if err != nil {
		return MacroMarketSnapshot{}, err
	}
	if len(rows) == 0 {
		return MacroMarketSnapshot{}, sql.ErrNoRows
	}
	first := rows[0]
	return MacroMarketSnapshot{
		TradeDate:      tradeDate,
		RiskScore:      first.RiskScore,
		CommodityScore: first.CommodityScore,
		LiquidityScore: first.LiquidityScore,
		TotalScore:     first.TotalScore,
		MarketLevel:    first.MarketLevel,
		MarketStatus:   first.MarketStatus,
		Summary:        first.Summary,
		Rows:           rows,
	}, nil
}

func queryMacroMarketHistory(ctx context.Context, db *sql.DB, limit int) ([]MacroMarketScore, error) {
	rows, err := db.QueryContext(ctx, `
SELECT DATE_FORMAT(trade_time, '%Y-%m-%d'),
       COALESCE(MAX(risk_score), 0),
       COALESCE(MAX(commodity_score), 0),
       COALESCE(MAX(liquidity_score), 0),
       COALESCE(MAX(total_score), 0),
       COALESCE(MAX(market_level), 0),
       COALESCE(MAX(market_status), ''),
       COALESCE(MAX(summary), '')
FROM macro_market_daily
GROUP BY DATE_FORMAT(trade_time, '%Y-%m-%d')
ORDER BY DATE_FORMAT(trade_time, '%Y-%m-%d') DESC
LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("query macro market history failed: %w", err)
	}
	defer rows.Close()

	result := make([]MacroMarketScore, 0, limit)
	for rows.Next() {
		var item MacroMarketScore
		if err := rows.Scan(&item.TradeDate, &item.RiskScore, &item.CommodityScore, &item.LiquidityScore, &item.TotalScore, &item.MarketLevel, &item.MarketStatus, &item.Summary); err != nil {
			return nil, fmt.Errorf("scan macro market history failed: %w", err)
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate macro market history failed: %w", err)
	}
	return result, nil
}

func queryMacroMarketRows(ctx context.Context, db *sql.DB, tradeDate string) ([]MacroMarketItem, error) {
	rows, err := db.QueryContext(ctx, `
SELECT symbol, market_name, market_type,
       COALESCE(close_price, 0), COALESCE(max_price, 0), COALESCE(min_price, 0),
       COALESCE(rise, 0), COALESCE(volume, 0),
       DATE_FORMAT(trade_time, '%Y-%m-%d'),
       COALESCE(risk_score, 0), COALESCE(commodity_score, 0), COALESCE(liquidity_score, 0),
       COALESCE(total_score, 0), COALESCE(market_level, 0), COALESCE(market_status, ''),
       COALESCE(summary, ''),
       COALESCE(note, '')
FROM macro_market_daily
WHERE trade_time >= ? AND trade_time <= ?
ORDER BY FIELD(symbol, '^GSPC', '^IXIC', '^DJI', '^N225', '^KS11', 'GC=F', 'HG=F', 'CL=F', 'DX-Y.NYB', '^TNX')`,
		tradeDate+" 00:00:00", tradeDate+" 23:59:59")
	if err != nil {
		return nil, fmt.Errorf("query macro market rows failed: %w", err)
	}
	defer rows.Close()

	result := make([]MacroMarketItem, 0, len(macroMarketSymbols))
	for rows.Next() {
		var item MacroMarketItem
		if err := rows.Scan(&item.Symbol, &item.MarketName, &item.MarketType, &item.ClosePrice, &item.MaxPrice, &item.MinPrice, &item.Rise, &item.Volume, &item.TradeDate, &item.RiskScore, &item.CommodityScore, &item.LiquidityScore, &item.TotalScore, &item.MarketLevel, &item.MarketStatus, &item.Summary, &item.Note); err != nil {
			return nil, fmt.Errorf("scan macro market rows failed: %w", err)
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate macro market rows failed: %w", err)
	}
	return result, nil
}
