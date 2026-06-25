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
const coverSelectedBtn = document.querySelector("#coverSelectedBtn");
const refreshSelectedBtn = document.querySelector("#refreshSelectedBtn");
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
const aiMergePanelEl = document.querySelector("#aiMergePanel");
const qwenTextEl = document.querySelector("#qwenText");
const yuanbaoTextEl = document.querySelector("#yuanbaoText");
const chatgptTextEl = document.querySelector("#chatgptText");
const mergeAIButton = document.querySelector("#mergeAIButton");
const copyAIResultButton = document.querySelector("#copyAIResultButton");
const aiMergeHintEl = document.querySelector("#aiMergeHint");
const aiMergeRowsEl = document.querySelector("#aiMergeRows");

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
    coverApi: "/api/shadow-stocks/cover",
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
    refreshApi: "/api/watchlist-stocks/refresh",
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
  },
  aiMerge: {
    title: "AI评级汇总",
    desc: "粘贴千问、元宝、ChatGPT 的 A/B/C 分类结果，按统一规则合并",
    api: "",
    riseLabel: "最终分类",
    metricLabel: "A类数量",
    maxMetricLabel: "A类数量",
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
let aiMergeResult = [];

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
  if (poolName === "aiMerge") {
    renderAIMerge();
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
	document.body.classList.toggle("ai-merge-mode", poolName === "aiMerge");
  const isMacro = poolName === "macro";
  const isAIMerge = poolName === "aiMerge";
  const isWatchlist = poolName === "watchlist";
  const isStandalone = isMacro || isAIMerge;
  document.querySelector(".toolbar").style.display = isStandalone ? "none" : "";
  document.querySelector(".summary").style.display = isStandalone ? "none" : "";
  document.querySelector(".table-wrap").style.display = isStandalone ? "none" : "";
  document.querySelector(".pager").style.display = isStandalone ? "none" : "";
  document.querySelectorAll(".star-filter").forEach(el => {
    el.style.display = isWatchlist ? "none" : "";
  });
  exportBtn.style.display = isWatchlist ? "none" : "";
  coverSelectedBtn.style.display = poolName === "shadow" ? "" : "none";
  refreshSelectedBtn.style.display = isWatchlist ? "" : "none";
  maxAmountEl.nextElementSibling.textContent = isWatchlist ? "最高实时价" : "最高成交额(亿)";
  macroPanelEl.style.display = isMacro ? "grid" : "none";
  aiMergePanelEl.style.display = isAIMerge ? "block" : "none";
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

coverSelectedBtn.addEventListener("click", () => {
  runSelectedAction("cover");
});

refreshSelectedBtn.addEventListener("click", () => {
  runSelectedAction("refresh");
});

mergeAIButton.addEventListener("click", () => {
  renderAIMerge();
});

copyAIResultButton.addEventListener("click", async () => {
  if (!aiMergeResult.length) {
    setAIMergeHint("没有可复制的结果");
    return;
  }
  const text = buildAIMergeMarkdown(aiMergeResult);
  try {
    await copyText(text);
    setAIMergeHint(`已复制 ${aiMergeResult.length} 条结果`);
  } catch (err) {
    setAIMergeHint(err.message);
  }
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
  coverSelectedBtn.disabled = currentPool !== "shadow" || checkedCount === 0;
  refreshSelectedBtn.disabled = currentPool !== "watchlist" || checkedCount === 0;
}

async function runSelectedAction(action) {
  const ids = getSelectedRowIDs();
  if (!ids.length) return;
  const isCover = action === "cover";
  const api = isCover ? pools.shadow.coverApi : pools.watchlist.refreshApi;
  const label = isCover ? "Cover" : "刷新最新价";
  const button = isCover ? coverSelectedBtn : refreshSelectedBtn;
  if (!window.confirm(`确认对选中的 ${ids.length} 条记录执行${label}？`)) return;
  button.disabled = true;
  setStatus(isCover ? "Covering" : "Refreshing");
  try {
    const res = await fetch(api, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ ids })
    });
    if (!res.ok) throw new Error(await res.text());
    const result = await res.json();
    setStatus(`Done updated=${toNumber(result.updated)} skipped=${toNumber(result.skipped)} failed=${toNumber(result.failed)}`);
    await loadRows();
  } catch (err) {
    setStatus(err.message);
  } finally {
    updateSelectionState();
  }
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

function renderAIMerge() {
  const qwen = parseAIRatingText(qwenTextEl.value, "qwen");
  const yuanbao = parseAIRatingText(yuanbaoTextEl.value, "yuanbao");
  const chatgpt = parseAIRatingText(chatgptTextEl.value, "chatgpt");
  const codes = Array.from(new Set([...qwen.keys(), ...yuanbao.keys(), ...chatgpt.keys()])).sort();
  aiMergeResult = codes.map(code => {
    const qwenItem = qwen.get(code) || {};
    const yuanbaoItem = yuanbao.get(code) || {};
    const chatgptItem = chatgpt.get(code) || {};
    const levels = {
      qwen: qwenItem.level || "",
      yuanbao: yuanbaoItem.level || "",
      chatgpt: chatgptItem.level || ""
    };
    const merged = mergeAILevels(levels);
    return {
      code,
      name: qwenItem.name || yuanbaoItem.name || chatgptItem.name || "",
      qwen: levels.qwen,
      yuanbao: levels.yuanbao,
      chatgpt: levels.chatgpt,
      finalLevel: merged.level,
      reason: merged.reason
    };
  });
  renderAIMergeRows(aiMergeResult);
}

function parseAIRatingText(text) {
	const result = new Map();
	let currentLevel = "";
	const lines = String(text || "")
		.replace(/\r/g, "")
		.split("\n")
		.map(line => line.trim())
		.filter(Boolean);
	for (const line of lines) {
		const headingLevel = extractAIHeadingLevel(line);
		if (headingLevel) {
			currentLevel = headingLevel;
			continue;
		}
		const codes = Array.from(line.matchAll(/\b(\d{6})\b/g)).map(match => match[1]);
		if (!codes.length) continue;
		const level = extractAILevel(line) || currentLevel;
		if (!level) continue;
		for (const code of codes) {
			const next = {
				code,
				name: extractStockName(line, code),
				level
			};
			const previous = result.get(code);
			result.set(code, mergeSameModelRating(previous, next));
		}
	}
	return result;
}

function extractAIHeadingLevel(text) {
	const value = String(text || "")
		.replace(/[ＡＢＣ]/g, char => String.fromCharCode(char.charCodeAt(0) - 65248))
		.replace(/^[^ABCabc]*/g, "")
		.trim();
	const match = value.match(/^(?:#+\s*)?([ABC])\s*类(?:[（(：:\s]|$)/i);
	if (match) return match[1].toUpperCase();
	return "";
}

function extractAILevel(text) {
	const value = String(text || "").replace(/[ＡＢＣ]/g, char => String.fromCharCode(char.charCodeAt(0) - 65248));
	const tableCells = value.split("|").map(cell => cell.trim().toUpperCase());
	const tableLevel = tableCells.find(cell => /^[ABC](类|级|档)?$/.test(cell));
	if (tableLevel) return tableLevel[0];

  const patterns = [
    /(?:评级|分类|等级|结论|类别|综合|最终|模型评级|AI评级)\s*[:：为是-]?\s*([ABC])\s*(?:类|级|档)?/i,
    /(?:归为|输出为|判定为|定为)\s*([ABC])\s*(?:类|级|档)/i,
    /([ABC])\s*(?:类|级|档)/i
	];
	for (const pattern of patterns) {
		const match = value.match(pattern);
		if (match) return match[1].toUpperCase();
	}
	const tokens = value.trim().split(/\s+/).map(token => token.toUpperCase());
	const tokenLevel = tokens.find(token => /^[ABC](?:类|级|档|边缘)?$/.test(token));
	if (tokenLevel) return tokenLevel[0];
	return "";
}

function mergeSameModelRating(previous, next) {
	if (!previous) return next;
	const rank = { A: 1, B: 2, C: 3 };
	const previousRank = rank[previous.level] || 0;
	const nextRank = rank[next.level] || 0;
	if (nextRank > previousRank) {
		return {
			...next,
			name: previous.name || next.name
		};
	}
	return {
		...previous,
		name: previous.name || next.name
	};
}

function extractStockName(text, code) {
  const clean = String(text || "")
    .replace(/\|/g, " ")
    .replace(/[*#`：:，,。；;]/g, " ")
    .replace(/\s+/g, " ")
    .trim();
  const beforePattern = new RegExp(`([\\u4e00-\\u9fa5A-Za-z*ST]{2,16})\\s+${code}`);
  const beforeMatch = clean.match(beforePattern);
  if (beforeMatch) return beforeMatch[1];
  const afterPattern = new RegExp(`${code}\\s+([\\u4e00-\\u9fa5A-Za-z*ST]{2,16})`);
  const afterMatch = clean.match(afterPattern);
  if (afterMatch) return afterMatch[1];
  return "";
}

function mergeAILevels(levels) {
  const values = [levels.qwen, levels.yuanbao, levels.chatgpt].filter(Boolean);
  const hasA = values.includes("A");
  const hasC = values.includes("C");
  if (!hasA) {
    return { level: "C", reason: "没有任何模型给A" };
  }
  if (hasC) {
    return { level: "B", reason: "A与C同时出现，降为B" };
  }
  return { level: "A", reason: "至少一个A，且没有C" };
}

function renderAIMergeRows(rows) {
  const rank = { A: 0, B: 1, C: 2 };
  const sorted = rows.slice().sort((a, b) => (rank[a.finalLevel] - rank[b.finalLevel]) || a.code.localeCompare(b.code));
  aiMergeRowsEl.innerHTML = sorted.map(row => `
    <tr>
      <td class="code-cell">${row.code}</td>
      <td>${row.name || "-"}</td>
      <td class="ai-level-${row.qwen || "empty"}">${row.qwen || "-"}</td>
      <td class="ai-level-${row.yuanbao || "empty"}">${row.yuanbao || "-"}</td>
      <td class="ai-level-${row.chatgpt || "empty"}">${row.chatgpt || "-"}</td>
      <td class="ai-final ai-level-${row.finalLevel}">${row.finalLevel}</td>
      <td class="muted">${row.reason}</td>
    </tr>
  `).join("");
  const aCount = rows.filter(row => row.finalLevel === "A").length;
  const bCount = rows.filter(row => row.finalLevel === "B").length;
  const cCount = rows.filter(row => row.finalLevel === "C").length;
  setAIMergeHint(`共 ${rows.length} 条，A类 ${aCount}，B类 ${bCount}，C类 ${cCount}`);
}

function buildAIMergeMarkdown(rows) {
  const sorted = rows.slice().sort((a, b) => a.finalLevel.localeCompare(b.finalLevel) || a.code.localeCompare(b.code));
  const lines = ["| 代码 | 名称 | 千问 | 元宝 | ChatGPT | 最终分类 | 规则命中 |", "| --- | --- | --- | --- | --- | --- | --- |"];
  for (const row of sorted) {
    lines.push(`| ${row.code} | ${row.name || "-"} | ${row.qwen || "-"} | ${row.yuanbao || "-"} | ${row.chatgpt || "-"} | ${row.finalLevel} | ${row.reason} |`);
  }
  return lines.join("\n");
}

function setAIMergeHint(text) {
  aiMergeHintEl.textContent = text;
  setStatus(text);
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
