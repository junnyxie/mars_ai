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

const pools = {
  volume: {
    title: "放量股票池",
    desc: "量比突破股票监控",
    api: "/api/volume-stocks",
    deleteApi: "/api/volume-stocks/delete",
    startApi: "/api/volume-stocks/start",
    riseLabel: "涨跌幅",
    metricLabel: "量比",
    maxMetricLabel: "最高量比",
    metricSuffix: ""
  },
  shadow: {
    title: "上影线试盘池",
    desc: "上影率来自 shadow_stock.shadow_rate，收盘涨幅来自 shadow_stock.raise_rate",
    api: "/api/shadow-stocks",
    deleteApi: "/api/shadow-stocks/delete",
    startApi: "/api/shadow-stocks/start",
    riseLabel: "收盘涨幅",
    metricLabel: "上影率",
    maxMetricLabel: "最高上影率",
    metricSuffix: "%"
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

function amountYiToRaw(value) {
  if (value === "") return "";
  return String(toNumber(value) * 100000000);
}

function setStatus(text) {
  statusEl.textContent = text;
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
  const seq = ++requestSeq;
  setStatus("Loading");

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
  if (poolName === "shadow" && coverBelowEl.checked) params.set("cover_below", "1");
  if (starredOnlyEl.checked) params.set("starred", "1");

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
	const pool = pools[poolName];
	const shadowCells = row => poolName === "shadow" ? `
      <td>${formatCoverPrice(row.first_cover_price)}</td>
      <td class="muted">${formatCoverTime(row.first_cover_time)}</td>
      <td>${formatCoverPrice(row.now_cover_price)}</td>
      <td class="muted">${formatCoverTime(row.now_cover_time)}</td>
    ` : "";
	rowsEl.innerHTML = rows.map(row => `
    <tr>
      <td class="select-col"><input class="row-check" type="checkbox" value="${row.id}" aria-label="选择 ${row.stock_code}" /></td>
      <td class="star-col"><button class="star-btn ${toNumber(row.start) === 1 ? "active" : ""}" type="button" data-id="${row.id}" data-start="${toNumber(row.start) === 1 ? 1 : 0}" aria-label="标星 ${row.stock_code}">★</button></td>
      <td class="code-cell">${row.stock_code}</td>
      <td>${row.stock_name}</td>
      <td>${row.sector_name || ""}</td>
      <td>${formatNumber(row.close_price)}</td>
      <td>${formatNumber(row.max_price)}</td>
      <td>${formatNumber(row.min_price)}</td>
      <td class="${toNumber(row.rise) >= 0 ? "rise-up" : ""}">${formatNumber(row.rise)}%</td>
      <td class="metric-cell">${formatNumber(row.vol)}${pool.metricSuffix}</td>
      <td>${formatAmountYi(row.amount)}</td>
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

function applyPool(poolName) {
  if (!pools[poolName]) return;
  currentPool = poolName;
  if (window.location.hash !== `#${poolName}`) {
    window.location.hash = poolName;
  }
  sortField = defaultSortField;
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
	resetSummary();
  updateSortArrows();
  loadRows();
}

document.querySelector("#searchBtn").addEventListener("click", () => {
  currentPage = 1;
  loadRows();
});

coverBelowEl.addEventListener("change", () => {
  currentPage = 1;
  loadRows();
});

starredOnlyEl.addEventListener("change", () => {
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

startDateEl.value = today();
endDateEl.value = today();
applyPool(window.location.hash.replace("#", "") || "volume");
