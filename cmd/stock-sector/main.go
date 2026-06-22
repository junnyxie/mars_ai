package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"log"
	"time"

	"mars_ai/internal/config"
	"mars_ai/internal/db"
	"mars_ai/internal/logging"
	"mars_ai/internal/spider"
	"mars_ai/internal/yahoo"
)

func main() {
	configPath := flag.String("config", "config/config.json", "config file path")
	dryRun := flag.Bool("dry-run", false, "crawl and print data without writing mysql")
	sectorOnly := flag.Bool("sector-only", false, "only crawl and save stock_sector")
	stockOnly := flag.Bool("stock-only", false, "load stock_sector from mysql, then crawl and save stock only")
	checkCookie := flag.Bool("check-cookie", false, "generate v cookie from v_js_path and request one Tonghuashun ajax page")
	checkYahoo := flag.Bool("check-yahoo", false, "load first SH/SZ stock and print Yahoo daily volume struct")
	serve := flag.Bool("serve", false, "start volume_stock service")
	flag.Parse()

	if *sectorOnly && *stockOnly {
		log.Fatalf("--sector-only and --stock-only cannot be used together")
	}

	backendLog, err := logging.InitBackendLogger()
	if err != nil {
		log.Fatalf("init backend logger failed: %v", err)
	}
	defer backendLog.Close()

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("load config failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	if *checkCookie {
		if err := spider.CheckTonghuashunVCookie(ctx, cfg.Tonghuashun); err != nil {
			log.Fatalf("check tonghuashun v cookie failed: %v", err)
		}
		fmt.Println("check tonghuashun v cookie success")
		return
	}

	if *checkYahoo {
		mysqlDB, err := db.Open(cfg.MySQL)
		if err != nil {
			log.Fatalf("open mysql failed: %v", err)
		}
		defer mysqlDB.Close()

		volume, err := yahoo.CheckFirstStock(ctx, mysqlDB)
		if err != nil {
			log.Fatalf("check yahoo failed: %v", err)
		}
		fmt.Printf("%+v\n", volume)
		return
	}

	if *serve {
		mysqlDB, err := db.Open(cfg.MySQL)
		if err != nil {
			log.Fatalf("open mysql failed: %v", err)
		}
		defer mysqlDB.Close()
		runner := yahoo.NewVolumeRunner(mysqlDB)
		if err := runner.ServeHTTP(cfg.Server.Addr); err != nil {
			log.Fatalf("serve volume failed: %v", err)
		}
		return
	}

	var mysqlDB *sql.DB
	var sectorDBIDs map[int64]int64
	var sectors []spider.StockSector

	if *stockOnly || !*dryRun {
		mysqlDB, err = db.Open(cfg.MySQL)
		if err != nil {
			log.Fatalf("open mysql failed: %v", err)
		}
		defer func(mysqlDB *sql.DB) {
			if err := mysqlDB.Close(); err != nil {
				log.Printf("close mysql failed: %v", err)
			}
		}(mysqlDB)
	}

	if *stockOnly {
		sectors, sectorDBIDs, err = spider.LoadStockSectors(ctx, mysqlDB)
		if err != nil {
			log.Fatalf("load stock sectors failed: %v", err)
		}
	} else {
		sectors, err = spider.CrawlSectorData(ctx, cfg.Tonghuashun)
		if err != nil {
			log.Fatalf("crawl sector data failed: %v", err)
		}
	}

	if *dryRun && *sectorOnly {
		for _, sector := range sectors {
			fmt.Printf("%d\t%s\n", sector.SectorID, sector.SectorName)
		}
		fmt.Printf("total sectors: %d\n", len(sectors))
		return
	}

	if *dryRun && !*stockOnly {
		sectorDBIDs = make(map[int64]int64, len(sectors))
		for _, sector := range sectors {
			sectorDBIDs[sector.SectorID] = sector.SectorID
		}
	}

	if !*dryRun && !*stockOnly {
		sectorDBIDs, err = spider.SaveStockSectors(ctx, mysqlDB, sectors)
		if err != nil {
			log.Fatalf("save stock sectors failed: %v", err)
		}
	}

	if *sectorOnly {
		fmt.Printf("saved stock sectors: %d\n", len(sectors))
		return
	}

	if !*dryRun {
		result, err := spider.SyncStocksByPage(ctx, mysqlDB, cfg.Tonghuashun, sectors, sectorDBIDs)
		if err != nil {
			log.Fatalf("sync stocks failed: %v", err)
		}
		if *stockOnly {
			fmt.Printf("loaded stock sectors: %d\n", len(sectors))
		} else {
			fmt.Printf("saved stock sectors: %d\n", len(sectors))
		}
		fmt.Printf("synced sectors: %d\n", result.SectorCount)
		fmt.Printf("synced stock rows: %d\n", result.StockCount)
		return
	}

	stocks, err := spider.CrawlStockData(ctx, cfg.Tonghuashun, sectors, sectorDBIDs)
	if err != nil {
		log.Fatalf("crawl stock data failed: %v", err)
	}
	if *dryRun {
		for _, stock := range stocks {
			fmt.Printf("%s\t%s\t%s\t%d\t%s\n", stock.StockCode, stock.StockName, stock.Region.String, stock.SectorID, stock.SectorName.String)
		}
		fmt.Printf("total sectors: %d\n", len(sectors))
		fmt.Printf("total stocks: %d\n", len(stocks))
		return
	}
}
