const rowsEl = document.querySelector("#rows");
const statusEl = document.querySelector("#status");
const totalCountEl = document.querySelector("#totalCount");
const maxVolEl = document.querySelector("#maxVol");
const maxVolLabelEl = document.querySelector("#maxVolLabel");
const maxAmountEl = document.querySelector("#maxAmount");
const startDateEl = document.querySelector("#startDate");
const endDateEl = document.querySelector("#endDate");
const minAmountEl = document.querySelector("#minAmount");
const maxAmountInputEl = document.querySelector("#maxAmountInput");
const coverBelowEl = document.querySelector("#coverBelow");
const starredOnlyEl = document.querySelector("#starredOnly");
const gptStarredOnlyEl = document.querySelector("#gptStarredOnly");
const pageSizeEl = document.querySelector("#pageSize");
const prevPageEl = document.querySelector("#prevPage");
const nextPageEl = document.querySelector("#nextPage");
const pageInfoEl = document.querySelector("#pageInfo");
const pageTitleEl = document.querySelector("#pageTitle");
const pageDescEl = document.querySelector("#pageDesc");
const riseColumnLabelEl = document.querySelector("#riseColumnLabel");
const volColumnLabelEl = document.querySelector("#volColumnLabel");
const menuEl = document.querySelector(".menu");
const selectAllRowsEl = document.querySelector("#selectAllRows");
const deleteSelectedBtn = document.querySelector("#deleteSelectedBtn");
const exportBtn = document.querySelector("#exportBtn");
const macroPanelEl = document.querySelector("#macroPanel");
const macroDateEl = document.querySelector("#macroDate");
const macroScoreEl = document.querySelector("#macroScore");
const macroStatusEl = document.querySelector("#macroStatus");
const macroRiskEl = document.querySelector("#macroRisk");
const macroCommodityEl = document.querySelector("#macroCommodity");
const macroLiquidityEl = document.querySelector("#macroLiquidity");
const macroChartEl = document.querySelector("#macroChart");
const macroSelectedHintEl = document.querySelector("#macroSelectedHint");
const macroRowsEl = document.querySelector("#macroRows");
const macroSummaryEl = document.querySelector("#macroSummary");

const pools = {
  volume: {
    title: "放量股票池",
    desc: "量比突破股票监控",
    api: "/api/volume-stocks",
    deleteApi: "/api/volume-stocks/delete",
    startApi: "/api/volume-stocks/start",
    gptStarApi: "/api/volume-stocks/gpt-star",
    riseLabel: "涨跌幅",
    metricLabel: "量比",
    maxMetricLabel: "最高量比",
    metricSuffix: ""
  },
  shadow: {
    title: "上影线试盘池",
    desc: "最高价涨幅来自 shadow_stock.high_rate，收盘涨幅来自 shadow_stock.raise_rate",
    api: "/api/shadow-stocks",
    deleteApi: "/api/shadow-stocks/delete",
    startApi: "/api/shadow-stocks/start",
    gptStarApi: "/api/shadow-stocks/gpt-star",
    riseLabel: "收盘涨幅",
    metricLabel: "最高价涨幅",
    maxMetricLabel: "最高价涨幅",
    metricSuffix: "%"
  },
  breakout: {
    title: "突破股票池",
    desc: "近三日高点递增，收盘价与前高价接近，成交量接近前高日",
    api: "/api/breakout-stocks",
    deleteApi: "/api/breakout-stocks/delete",
    startApi: "/api/breakout-stocks/start",
    gptStarApi: "/api/breakout-stocks/gpt-star",
    riseLabel: "涨跌幅",
    metricLabel: "量能接近度",
    maxMetricLabel: "最高量能比",
    metricSuffix: ""
  },
  watchlist: {
    title: "监控股票池",
    desc: "手动标星和GPT标星同时满足后加入，跟踪加入后的实时价格表现",
    api: "/api/watchlist-stocks",
    deleteApi: "/api/watchlist-stocks/delete",
    riseLabel: "监控涨跌幅",
    metricLabel: "涨跌幅",
    maxMetricLabel: "最高涨幅",
    metricSuffix: "%",
    defaultSort: "join_time"
  },
  macro: {
    title: "宏观数据",
    desc: "全球指数、商品、美元和美债收益率的宏观强弱评分",
    api: "/api/macro-market",
    riseLabel: "涨跌幅",
    metricLabel: "宏观评分",
    maxMetricLabel: "总评分",
    metricSuffix: ""
  }
};

const defaultSortField = "gmt_create";
const defaultSortDir = "desc";
let currentPool = "volume";
let sortField = defaultSortField;
let sortDir = defaultSortDir;
let requestSeq = 0;
let currentPage = 1;
let totalRows = 0;

function today() {
  return new Date().toISOString().slice(0, 10);
}

function toNumber(value) {
  const parsed = Number(String(value == null ? 0 : value).replace(/,/g, ""));
  return Number.isFinite(parsed) ? parsed : 0;
}

function formatNumber(value, digits = 2) {
  return toNumber(value).toLocaleString("zh-CN", {
    minimumFractionDigits: digits,
    maximumFractionDigits: digits
  });
}

function formatAmountYi(value) {
  return `${formatNumber(toNumber(value) / 100000000, 2)}亿`;
}

function formatCoverPrice(value) {
  const price = toNumber(value);
  return price > 0 ? formatNumber(price, 2) : "-";
}

function formatCoverTime(value) {
  return value ? String(value).slice(0, 10) : "-";
}

function xueqiuSymbol(stockCode) {
  const code = String(stockCode || "").trim();
  if (!code) return "";
  const prefix = code.startsWith("6") ? "SH" : "SZ";
  return `${prefix}${code}`;
}

function xueqiuLink(stockCode) {
  const symbol = xueqiuSymbol(stockCode);
  return symbol ? `https://xueqiu.com/S/${symbol}` : "";
}

function amountYiToRaw(value) {
  if (value === "") return "";
  return String(toNumber(value) * 100000000);
}

function setStatus(text) {
  statusEl.textContent = text;
}

function buildQueryParams() {
  const params = new URLSearchParams({
    start: startDateEl.value,
    end: endDateEl.value,
    sort: sortField,
    dir: sortDir,
    page: String(currentPage),
    page_size: pageSizeEl.value
  });
  if (minAmountEl.value !== "") params.set("min_amount", amountYiToRaw(minAmountEl.value));
  if (maxAmountInputEl.value !== "") params.set("max_amount", amountYiToRaw(maxAmountInputEl.value));
  if (currentPool === "shadow" && coverBelowEl.checked) params.set("cover_below", "1");
  if (starredOnlyEl.checked) params.set("starred", "1");
  if (gptStarredOnlyEl.checked) params.set("gpt_starred", "1");
  return params;
}

function resetSummary() {
  rowsEl.innerHTML = "";
  selectAllRowsEl.checked = false;
  selectAllRowsEl.indeterminate = false;
  deleteSelectedBtn.disabled = true;
  totalCountEl.textContent = "0";
  maxVolEl.textContent = "-";
  maxAmountEl.textContent = "-";
  pageInfoEl.textContent = "第 1 / 1 页";
  prevPageEl.disabled = true;
  nextPageEl.disabled = true;
}

async function loadRows() {
  const poolName = currentPool;
  const pool = pools[poolName];
  if (poolName === "macro") {
    await loadMacroMarket();
    return;
  }
  const seq = ++requestSeq;
  setStatus("Loading");

  const params = buildQueryParams();

  try {
    const res = await fetch(`${pool.api}?${params}`);
    if (!res.ok) throw new Error(await res.text());
    const data = await res.json();
    if (seq !== requestSeq || poolName !== currentPool) return;
    totalRows = toNumber(data.total);
    currentPage = toNumber(data.page) || currentPage;
    render(data.rows || [], poolName);
    setStatus("Ready");
  } catch (err) {
    if (seq === requestSeq && poolName === currentPool) {
      setStatus(err.message);
    }
  }
}

function render(rows, poolName) {
  if (poolName === "watchlist") {
    renderWatchlist(rows);
    return;
  }
	const pool = pools[poolName];
	const stockCodeCell = row => {
		const href = xueqiuLink(row.stock_code);
		if (!href) return row.stock_code || "";
		return `<a class="stock-link" href="${href}" target="_blank" rel="noopener noreferrer">${row.stock_code}</a>`;
	};
	const shadowCells = row => poolName === "shadow" ? `
      <td>${formatCoverPrice(row.first_cover_price)}</td>
      <td class="muted">${formatCoverTime(row.first_cover_time)}</td>
      <td>${formatCoverPrice(row.now_cover_price)}</td>
      <td class="muted">${formatCoverTime(row.now_cover_time)}</td>
    ` : "";
	const breakoutCells = row => poolName === "breakout" ? `
      <td>${formatNumber(row.before_max_price)}</td>
      <td>${formatNumber(row.before_max_vol, 0)}</td>
      <td class="muted">${formatCoverTime(row.before_max_time)}</td>
    ` : "";
	rowsEl.innerHTML = rows.map(row => `
    <tr>
      <td class="select-col"><input class="row-check" type="checkbox" value="${row.id}" aria-label="选择 ${row.stock_code}" /></td>
      <td class="star-col"><button class="star-btn ${toNumber(row.start) === 1 ? "active" : ""}" type="button" data-id="${row.id}" data-start="${toNumber(row.start) === 1 ? 1 : 0}" aria-label="标星 ${row.stock_code}">★</button></td>
      <td class="star-col"><button class="gpt-star-btn ${toNumber(row.gpt_star) === 1 ? "active" : ""}" type="button" data-id="${row.id}" data-gpt-star="${toNumber(row.gpt_star) === 1 ? 1 : 0}" aria-label="GPT标星 ${row.stock_code}">G</button></td>
      <td class="code-cell">${stockCodeCell(row)}</td>
      <td>${row.stock_name}</td>
      <td>${row.sector_name || ""}</td>
      <td>${formatNumber(row.close_price)}</td>
      <td>${formatNumber(row.max_price)}</td>
      <td>${formatNumber(row.min_price)}</td>
      <td class="${toNumber(row.rise) >= 0 ? "rise-up" : ""}">${formatNumber(row.rise)}%</td>
      <td class="metric-cell">${formatNumber(row.vol)}${pool.metricSuffix}</td>
      <td>${formatAmountYi(row.amount)}</td>
      ${breakoutCells(row)}
      ${shadowCells(row)}
      <td class="muted">${row.gmt_create}</td>
      <td class="actions-col"><button class="row-delete" type="button" data-id="${row.id}">删除</button></td>
    </tr>
  `).join("");
  selectAllRowsEl.checked = false;
  selectAllRowsEl.indeterminate = false;
  updateSelectionState();

  totalCountEl.textContent = totalRows;
  const maxMetric = rows.length ? Math.max(...rows.map(row => toNumber(row.vol))) : null;
  maxVolEl.textContent = maxMetric === null ? "-" : `${formatNumber(maxMetric)}${pool.metricSuffix}`;
  maxAmountEl.textContent = rows.length ? formatAmountYi(Math.max(...rows.map(row => toNumber(row.amount)))) : "-";
  updatePager();
  updateSortArrows();
}

function sourcePoolLabel(value) {
  if (value === "shadow") return "上影线";
  if (value === "breakout") return "突破";
  return value || "-";
}

function renderWatchlist(rows) {
  const stockCodeCell = row => {
    const href = xueqiuLink(row.stock_code);
    if (!href) return row.stock_code || "";
    return `<a class="stock-link" href="${href}" target="_blank" rel="noopener noreferrer">${row.stock_code}</a>`;
  };
  rowsEl.innerHTML = rows.map(row => `
    <tr>
      <td class="select-col"><input class="row-check" type="checkbox" value="${row.id}" aria-label="选择 ${row.stock_code}" /></td>
      <td class="star-col pool-only"></td>
      <td class="star-col pool-only"></td>
      <td class="code-cell">${stockCodeCell(row)}</td>
      <td>${row.stock_name}</td>
      <td>${row.sector_name || ""}</td>
      <td class="watchlist-only">${sourcePoolLabel(row.source_pool)}</td>
      <td class="watchlist-only muted">${formatCoverTime(row.join_time)}</td>
      <td class="watchlist-only">${formatNumber(row.join_price)}</td>
      <td class="watchlist-only">${formatNumber(row.current_price)}</td>
      <td class="watchlist-only muted">${formatCoverTime(row.current_time)}</td>
      <td class="watchlist-only ${toNumber(row.rise) >= 0 ? "rise-up" : ""}">${formatNumber(row.rise)}%</td>
      <td class="pool-only"></td>
      <td class="pool-only"></td>
      <td class="pool-only"></td>
      <td class="pool-only"></td>
      <td class="pool-only"></td>
      <td class="pool-only"></td>
      <td class="breakout-only"></td>
      <td class="breakout-only"></td>
      <td class="breakout-only"></td>
      <td class="shadow-only"></td>
      <td class="shadow-only"></td>
      <td class="shadow-only"></td>
      <td class="shadow-only"></td>
      <td class="pool-only muted">${row.gmt_create || ""}</td>
      <td class="actions-col"><button class="row-delete" type="button" data-id="${row.id}">删除</button></td>
    </tr>
  `).join("");
  selectAllRowsEl.checked = false;
  selectAllRowsEl.indeterminate = false;
  updateSelectionState();

  totalCountEl.textContent = totalRows;
  const maxRise = rows.length ? Math.max(...rows.map(row => toNumber(row.rise))) : null;
  maxVolEl.textContent = maxRise === null ? "-" : `${formatNumber(maxRise)}%`;
  maxAmountEl.textContent = rows.length ? formatNumber(Math.max(...rows.map(row => toNumber(row.current_price)))) : "-";
  updatePager();
  updateSortArrows();
}

function applyPool(poolName) {
  if (!pools[poolName]) return;
  currentPool = poolName;
  if (window.location.hash !== `#${poolName}`) {
    window.location.hash = poolName;
  }
  sortField = pools[poolName].defaultSort || defaultSortField;
  sortDir = defaultSortDir;
  currentPage = 1;
  totalRows = 0;
  const pool = pools[currentPool];

  pageTitleEl.textContent = pool.title;
  pageDescEl.textContent = pool.desc;
  riseColumnLabelEl.textContent = pool.riseLabel;
  volColumnLabelEl.textContent = pool.metricLabel;
  maxVolLabelEl.textContent = pool.maxMetricLabel;
	document.querySelectorAll(".menu-item").forEach(item => {
		item.classList.toggle("active", item.dataset.pool === poolName);
	});
	document.body.classList.toggle("shadow-mode", poolName === "shadow");
	document.body.classList.toggle("breakout-mode", poolName === "breakout");
	document.body.classList.toggle("watchlist-mode", poolName === "watchlist");
	document.body.classList.toggle("macro-mode", poolName === "macro");
  const isMacro = poolName === "macro";
  const isWatchlist = poolName === "watchlist";
  document.querySelector(".toolbar").style.display = isMacro ? "none" : "";
  document.querySelector(".summary").style.display = isMacro ? "none" : "";
  document.querySelector(".table-wrap").style.display = isMacro ? "none" : "";
  document.querySelector(".pager").style.display = isMacro ? "none" : "";
  document.querySelectorAll(".star-filter").forEach(el => {
    el.style.display = isWatchlist ? "none" : "";
  });
  exportBtn.style.display = isWatchlist ? "none" : "";
  maxAmountEl.nextElementSibling.textContent = isWatchlist ? "最高实时价" : "最高成交额(亿)";
  macroPanelEl.style.display = isMacro ? "grid" : "none";
	resetSummary();
  updateSortArrows();
  loadRows();
}

document.querySelector("#searchBtn").addEventListener("click", () => {
  currentPage = 1;
  loadRows();
});

exportBtn.addEventListener("click", () => {
  exportForChatGPT();
});

coverBelowEl.addEventListener("change", () => {
  currentPage = 1;
  loadRows();
});

starredOnlyEl.addEventListener("change", () => {
  currentPage = 1;
  loadRows();
});

gptStarredOnlyEl.addEventListener("change", () => {
  currentPage = 1;
  loadRows();
});

menuEl.addEventListener("click", event => {
  const item = event.target.closest(".menu-item");
  if (!item) return;
  applyPool(item.dataset.pool);
});

document.querySelectorAll("th[data-sort]").forEach(th => {
  th.addEventListener("click", () => {
    const field = th.dataset.sort;
    if (sortField === field) {
      if (sortDir === "desc") {
        sortDir = "asc";
      } else {
        sortField = defaultSortField;
        sortDir = defaultSortDir;
      }
    } else {
      sortField = field;
      sortDir = "desc";
    }
    currentPage = 1;
    loadRows();
  });
});

pageSizeEl.addEventListener("change", () => {
  currentPage = 1;
  loadRows();
});

prevPageEl.addEventListener("click", () => {
  if (currentPage <= 1) return;
  currentPage -= 1;
  loadRows();
});

nextPageEl.addEventListener("click", () => {
  const totalPages = getTotalPages();
  if (currentPage >= totalPages) return;
  currentPage += 1;
  loadRows();
});

selectAllRowsEl.addEventListener("change", () => {
  document.querySelectorAll(".row-check").forEach(input => {
    input.checked = selectAllRowsEl.checked;
  });
  updateSelectionState();
});

deleteSelectedBtn.addEventListener("click", () => {
  deleteRows(getSelectedRowIDs());
});

rowsEl.addEventListener("change", event => {
  if (event.target.classList.contains("row-check")) {
    updateSelectionState();
  }
});

rowsEl.addEventListener("click", event => {
  const starButton = event.target.closest(".star-btn");
  if (starButton) {
    toggleStart(starButton);
    return;
  }
  const gptStarButton = event.target.closest(".gpt-star-btn");
  if (gptStarButton) {
    toggleGPTStar(gptStarButton);
    return;
  }
  const button = event.target.closest(".row-delete");
  if (!button) return;
  deleteRows([toNumber(button.dataset.id)]);
});

function getTotalPages() {
  return Math.max(1, Math.ceil(totalRows / toNumber(pageSizeEl.value || 50)));
}

function updatePager() {
  const totalPages = getTotalPages();
  pageInfoEl.textContent = `第 ${currentPage} / ${totalPages} 页，共 ${totalRows} 条`;
  prevPageEl.disabled = currentPage <= 1;
  nextPageEl.disabled = currentPage >= totalPages;
}

function updateSortArrows() {
  document.querySelectorAll("th[data-sort]").forEach(th => {
    const arrow = th.querySelector(".sort-arrow");
    if (!arrow) return;
    arrow.textContent = th.dataset.sort === sortField ? (sortDir === "desc" ? "↓" : "↑") : "";
  });
}

function getSelectedRowIDs() {
  return Array.from(document.querySelectorAll(".row-check:checked"))
    .map(input => toNumber(input.value))
    .filter(id => id > 0);
}

function updateSelectionState() {
  const checks = Array.from(document.querySelectorAll(".row-check"));
  const checkedCount = checks.filter(input => input.checked).length;
  selectAllRowsEl.checked = checks.length > 0 && checkedCount === checks.length;
  selectAllRowsEl.indeterminate = checkedCount > 0 && checkedCount < checks.length;
  deleteSelectedBtn.disabled = checkedCount === 0;
}

async function deleteRows(ids) {
  if (!ids.length) return;
  const pool = pools[currentPool];
  const message = ids.length === 1 ? "确认删除这条记录？" : `确认删除选中的 ${ids.length} 条记录？`;
  if (!window.confirm(message)) return;
  setStatus("Deleting");
  try {
    const res = await fetch(pool.deleteApi, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ ids })
    });
    if (!res.ok) throw new Error(await res.text());
    await res.json();
    if (rowsEl.children.length === ids.length && currentPage > 1) {
      currentPage -= 1;
    }
    await loadRows();
  } catch (err) {
    setStatus(err.message);
  }
}

async function toggleStart(button) {
  const id = toNumber(button.dataset.id);
  if (!id) return;
  const nextStart = toNumber(button.dataset.start) === 1 ? 0 : 1;
  const previousStart = toNumber(button.dataset.start) === 1 ? 1 : 0;
  button.disabled = true;
  button.dataset.start = String(nextStart);
  button.classList.toggle("active", nextStart === 1);
  setStatus("Saving");
  try {
    const res = await fetch(pools[currentPool].startApi, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ id, start: nextStart })
    });
    if (!res.ok) throw new Error(await res.text());
    setStatus("Ready");
    if (starredOnlyEl.checked && nextStart === 0) {
      await loadRows();
    }
  } catch (err) {
    button.dataset.start = String(previousStart);
    button.classList.toggle("active", previousStart === 1);
    setStatus(err.message);
  } finally {
    button.disabled = false;
  }
}

async function toggleGPTStar(button) {
  const id = toNumber(button.dataset.id);
  if (!id) return;
  const nextStar = toNumber(button.dataset.gptStar) === 1 ? 0 : 1;
  const previousStar = toNumber(button.dataset.gptStar) === 1 ? 1 : 0;
  button.disabled = true;
  button.dataset.gptStar = String(nextStar);
  button.classList.toggle("active", nextStar === 1);
  setStatus("Saving");
  try {
    const res = await fetch(pools[currentPool].gptStarApi, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ id, gpt_star: nextStar })
    });
    if (!res.ok) throw new Error(await res.text());
    setStatus("Ready");
    if (gptStarredOnlyEl.checked && nextStar === 0) {
      await loadRows();
    }
  } catch (err) {
    button.dataset.gptStar = String(previousStar);
    button.classList.toggle("active", previousStar === 1);
    setStatus(err.message);
  } finally {
    button.disabled = false;
  }
}

async function exportForChatGPT() {
  const params = buildQueryParams();
  params.set("pool", currentPool);
  exportBtn.disabled = true;
  setStatus("Exporting");
  try {
    const res = await fetch(`/api/stock-pool/export?${params}`);
    if (!res.ok) throw new Error(await res.text());
    const markdown = await res.text();
    await copyText(markdown);
    setStatus("Copied");
  } catch (err) {
    setStatus(err.message);
  } finally {
    exportBtn.disabled = false;
  }
}

async function copyText(text) {
  if (navigator.clipboard && window.isSecureContext) {
    await navigator.clipboard.writeText(text);
    return;
  }
  const textarea = document.createElement("textarea");
  textarea.value = text;
  textarea.setAttribute("readonly", "");
  textarea.style.position = "fixed";
  textarea.style.left = "-9999px";
  document.body.appendChild(textarea);
  textarea.select();
  document.execCommand("copy");
  textarea.remove();
}

async function loadMacroMarket() {
  const seq = ++requestSeq;
  setStatus("Loading");
  macroStatusEl.textContent = "Loading";
  macroRowsEl.innerHTML = "";
  macroChartEl.innerHTML = "";
  try {
    const res = await fetch("/api/macro-market?limit=60");
    if (!res.ok) throw new Error(await res.text());
    const data = await res.json();
    if (seq !== requestSeq || currentPool !== "macro") return;
    renderMacroMarket(data);
    setStatus("Ready");
  } catch (err) {
    if (seq === requestSeq && currentPool === "macro") {
      setStatus(err.message);
      macroDateEl.textContent = "-";
      macroScoreEl.textContent = "-";
      macroStatusEl.textContent = err.message;
      macroRiskEl.textContent = "-";
      macroCommodityEl.textContent = "-";
      macroLiquidityEl.textContent = "-";
      macroChartEl.innerHTML = `<div class="macro-empty">${err.message}</div>`;
      macroSelectedHintEl.textContent = "-";
      macroRowsEl.innerHTML = "";
      macroSummaryEl.textContent = "-";
    }
  }
}

function renderMacroMarket(data) {
  const latest = data.latest || {};
  if (!latest.trade_date) {
    macroDateEl.textContent = "-";
    macroScoreEl.textContent = "-";
    macroStatusEl.textContent = "暂无宏观数据";
    macroRiskEl.textContent = "-";
    macroCommodityEl.textContent = "-";
    macroLiquidityEl.textContent = "-";
    macroChartEl.innerHTML = `<div class="macro-empty">暂无历史评分，请先调用 /api/macro-market/run?days=1</div>`;
    macroSelectedHintEl.textContent = "-";
    macroRowsEl.innerHTML = "";
    macroSummaryEl.textContent = "-";
    return;
  }
  const history = (data.history || []).slice().reverse().slice(-60);
  const details = {};
  if (latest.trade_date) {
    details[latest.trade_date] = latest.rows || [];
  }
  const renderSnapshot = item => {
    const rows = details[item.trade_date] || item.rows || [];
    macroDateEl.textContent = item.trade_date || "-";
    macroScoreEl.textContent = item.total_score == null ? "-" : formatNumber(item.total_score, 2);
    macroStatusEl.textContent = item.market_status || "-";
    macroRiskEl.textContent = item.risk_score ?? "-";
    macroCommodityEl.textContent = item.commodity_score ?? "-";
    macroLiquidityEl.textContent = item.liquidity_score ?? "-";
    macroScoreEl.className = `macro-level-${item.market_level || 0}`;
    macroSelectedHintEl.textContent = `当前：${item.trade_date || "-"} · ${item.market_status || "-"} · ${formatNumber(item.total_score, 2)}`;
    macroSummaryEl.textContent = item.summary || "-";
    macroRowsEl.innerHTML = rows.map(row => `
      <div class="macro-item">
        <span>${row.market_name}</span>
        <strong>${formatNumber(row.close_price, 2)}</strong>
        <small class="${toNumber(row.rise) >= 0 ? "rise-up" : ""}">${formatNumber(row.rise, 2)}%</small>
      </div>
    `).join("");
  };

  renderSnapshot(latest);

  if (history.length === 0) {
    macroChartEl.innerHTML = `<div class="macro-empty">暂无历史评分</div>`;
  } else {
    const columns = Math.ceil(history.length / 3);
    macroChartEl.innerHTML = `
      <div class="macro-heatmap" style="grid-template-columns: repeat(${columns}, 18px);">
        ${history.map(item => `
          <button class="macro-cell ${item.trade_date === latest.trade_date ? "active" : ""} macro-level-${item.market_level || 0}" type="button" data-date="${item.trade_date}" title="${item.trade_date} ${formatNumber(item.total_score, 2)} ${item.market_status || ""}" aria-label="${item.trade_date} ${formatNumber(item.total_score, 2)} ${item.market_status || ""}">
          </button>
        `).join("")}
      </div>
    `;
    macroChartEl.querySelectorAll(".macro-cell").forEach(cell => {
      cell.addEventListener("click", async () => {
        const item = history.find(historyItem => historyItem.trade_date === cell.dataset.date);
        if (!item) return;
        macroChartEl.querySelectorAll(".macro-cell").forEach(node => node.classList.remove("active"));
        cell.classList.add("active");
        if (details[item.trade_date]) {
          renderSnapshot(item);
          return;
        }
        const previousHint = macroSelectedHintEl.textContent;
        macroSelectedHintEl.textContent = `加载中：${item.trade_date}`;
        try {
          const res = await fetch(`/api/macro-market/day?date=${encodeURIComponent(item.trade_date)}`);
          if (!res.ok) throw new Error(await res.text());
          const snapshot = await res.json();
          details[item.trade_date] = snapshot.rows || [];
          renderSnapshot(snapshot);
        } catch (err) {
          macroSelectedHintEl.textContent = previousHint;
          setStatus(err.message);
        }
      });
    });
  }
}

function macroBarHeight(score) {
  return Math.max(12, Math.min(100, Math.round((Math.abs(toNumber(score)) / 2) * 100)));
}

startDateEl.value = today();
endDateEl.value = today();
applyPool(window.location.hash.replace("#", "") || "volume");
