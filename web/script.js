
"use strict";

const STORAGE_KEY = "arazi-cikis-cetveli-v1";
const MONTHS = [
  "OCAK", "ŞUBAT", "MART", "NİSAN", "MAYIS", "HAZİRAN",
  "TEMMUZ", "AĞUSTOS", "EYLÜL", "EKİM", "KASIM", "ARALIK"
];
const WEEKDAYS = ["Pazar", "Pazartesi", "Salı", "Çarşamba", "Perşembe", "Cuma", "Cumartesi"];

const SEED_MONTHS = {};

const DEFAULT_SETTINGS = {
  preparedBy: "",
  preparedTitle: "",
  approvedBy: "",
  approvedTitle: ""
};

const currentDate = new Date();
const API_TOKEN = new URLSearchParams(window.location.search).get("token") || "";
let state = createBaseState();
let saveTimer = null;
let saveQueue = Promise.resolve();
let desktopMode = false;
let appInfo = null;

const yearSelect = document.getElementById("yearSelect");
const monthTabs = document.getElementById("monthTabs");
const entryBody = document.getElementById("entryBody");
const periodLabel = document.getElementById("periodLabel");
const saveStatus = document.getElementById("saveStatus");
const storageInfo = document.getElementById("storageInfo");
const firstLaunchNotice = document.getElementById("firstLaunchNotice");
const dismissWelcomeButton = document.getElementById("dismissWelcomeButton");

const preparedByInput = document.getElementById("preparedByInput");
const preparedTitleInput = document.getElementById("preparedTitleInput");
const approvedByInput = document.getElementById("approvedByInput");
const approvedTitleInput = document.getElementById("approvedTitleInput");

function clone(value) {
  return JSON.parse(JSON.stringify(value));
}

function createBaseState() {
  const year = currentDate.getFullYear();
  return {
    selectedYear: year,
    selectedMonth: currentDate.getMonth(),
    years: [year],
    months: clone(SEED_MONTHS),
    settings: clone(DEFAULT_SETTINGS)
  };
}

function normalizeState(saved) {
  const base = createBaseState();
  if (!saved || typeof saved !== "object") return base;

  return {
    selectedYear: Number(saved.selectedYear) || base.selectedYear,
    selectedMonth: Number.isInteger(saved.selectedMonth) ? saved.selectedMonth : base.selectedMonth,
    years: Array.isArray(saved.years)
      ? [...new Set(saved.years.map(Number).filter(Number.isFinite))].sort((a, b) => a - b)
      : base.years,
    months: saved.months && typeof saved.months === "object" ? saved.months : base.months,
    settings: { ...base.settings, ...(saved.settings || {}) }
  };
}

function apiUrl(path) {
  const separator = path.includes("?") ? "&" : "?";
  return `${path}${separator}token=${encodeURIComponent(API_TOKEN)}`;
}

async function loadStateFromDisk() {
  try {
    const response = await fetch(apiUrl("/api/state"), { cache: "no-store" });
    if (!response.ok) throw new Error(`HTTP ${response.status}`);

    const payload = await response.json();
    desktopMode = true;
    appInfo = payload;
    state = normalizeState(payload.state);

    if (storageInfo && payload.dataPath) {
      storageInfo.textContent = `SQLite otomatik kayıt etkin • Veritabanı: ${payload.dataPath} • Günlük .db yedekleme etkin`;
    }

    if (!payload.state) {
      await persistState(true);
    }
  } catch (error) {
    console.error("SQLite kayıt servisine ulaşılamadı:", error);
    desktopMode = false;
    state = createBaseState();
    saveStatus.textContent = "SQLite bağlantı hatası";
    if (storageInfo) {
      storageInfo.textContent = "Hata: SQLite veritabanına bağlanılamadı. Uygulamayı kapatıp yeniden açın.";
    }
  }
}

async function persistState(immediate = false) {
  const snapshot = JSON.stringify(state);
  saveStatus.textContent = "SQLite veritabanına kaydediliyor...";

  if (!desktopMode) {
    saveStatus.textContent = "SQLite bağlantı hatası";
    return;
  }

  const task = async () => {
    const response = await fetch(apiUrl("/api/state"), {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: snapshot,
      keepalive: true
    });
    if (!response.ok) throw new Error(`HTTP ${response.status}`);
    saveStatus.textContent = "SQLite veritabanına kaydedildi";
  };

  saveQueue = saveQueue.then(task, task).catch((error) => {
    console.error("SQLite kayıt yapılamadı:", error);
    saveStatus.textContent = "Kayıt hatası";
  });

  if (immediate) await saveQueue;
}

function saveState(immediate = false) {
  window.clearTimeout(saveTimer);
  if (immediate) {
    return persistState(true);
  }

  saveStatus.textContent = "Değişiklik bekliyor...";
  saveTimer = window.setTimeout(() => {
    persistState(false);
  }, 350);
}

function monthKey(year = state.selectedYear, monthIndex = state.selectedMonth) {
  return `${year}-${monthIndex + 1}`;
}

function createEmptyDay() {
  return {
    plate: "",
    place: "",
    subject: "",
    description: ""
  };
}

function ensureMonth(year = state.selectedYear, monthIndex = state.selectedMonth) {
  const key = monthKey(year, monthIndex);

  if (!state.months[key]) {
    state.months[key] = {
      days: {},
      preparedBy: state.settings.preparedBy,
      preparedTitle: state.settings.preparedTitle,
      approvedBy: state.settings.approvedBy,
      approvedTitle: state.settings.approvedTitle
    };
  }

  if (!state.months[key].days) {
    state.months[key].days = {};
  }

  return state.months[key];
}

function ensureDay(day) {
  const month = ensureMonth();
  const key = String(day);

  if (!month.days[key]) {
    month.days[key] = createEmptyDay();
  }

  return month.days[key];
}

function daysInMonth(year, monthIndex) {
  return new Date(year, monthIndex + 1, 0).getDate();
}

function isWeekend(year, monthIndex, day) {
  const weekday = new Date(year, monthIndex, day).getDay();
  return weekday === 0 || weekday === 6;
}

function hasDayData(dayData) {
  return ["plate", "place", "subject", "description"]
    .some((field) => String(dayData[field] || "").trim() !== "");
}

function formatDate(year, monthIndex, day) {
  return `${String(day).padStart(2, "0")}.${String(monthIndex + 1).padStart(2, "0")}.${year}`;
}

function toTurkishUpper(value) {
  return value.toLocaleUpperCase("tr-TR");
}

function setWeekendClass(row, day, dayData) {
  const weekend = isWeekend(state.selectedYear, state.selectedMonth, day);
  row.classList.toggle("weekend-empty", weekend && !hasDayData(dayData));
}

function buildYearOptions() {
  const candidateYears = new Set(state.years);
  Object.keys(state.months).forEach((key) => {
    const parsed = Number(key.split("-")[0]);
    if (Number.isFinite(parsed)) candidateYears.add(parsed);
  });

  const baseYear = currentDate.getFullYear();
  for (let year = baseYear - 3; year <= baseYear + 6; year += 1) {
    candidateYears.add(year);
  }

  candidateYears.add(state.selectedYear);
  state.years = [...candidateYears].sort((a, b) => a - b);

  yearSelect.innerHTML = "";
  state.years.forEach((year) => {
    const option = document.createElement("option");
    option.value = String(year);
    option.textContent = String(year);
    option.selected = year === state.selectedYear;
    yearSelect.appendChild(option);
  });
}

function renderMonthTabs() {
  monthTabs.innerHTML = "";

  MONTHS.forEach((month, index) => {
    const button = document.createElement("button");
    button.type = "button";
    button.className = "month-tab";
    button.textContent = month;
    button.classList.toggle("active", index === state.selectedMonth);

    button.addEventListener("click", () => {
      state.selectedMonth = index;
      saveState();
      render();
    });

    monthTabs.appendChild(button);
  });
}

function createInput(day, field, value, listId = null) {
  const input = document.createElement("input");
  input.type = "text";
  input.className = "cell-input";
  input.value = value || "";
  input.autocomplete = "off";
  input.dataset.day = String(day);
  input.dataset.field = field;
  input.placeholder = isWeekend(state.selectedYear, state.selectedMonth, day) ? "Hafta sonu" : "";

  if (listId) {
    input.setAttribute("list", listId);
  }

  input.addEventListener("input", (event) => {
    const dayData = ensureDay(day);
    dayData[field] = event.target.value;
    setWeekendClass(event.target.closest("tr"), day, dayData);
    saveState();
  });

  input.addEventListener("blur", (event) => {
    const normalized = toTurkishUpper(event.target.value.trim());
    event.target.value = normalized;
    ensureDay(day)[field] = normalized;
    setWeekendClass(event.target.closest("tr"), day, ensureDay(day));
    saveState();
    refreshSuggestions();
  });

  return input;
}

function renderRows() {
  entryBody.innerHTML = "";
  const totalDays = daysInMonth(state.selectedYear, state.selectedMonth);

  for (let day = 1; day <= totalDays; day += 1) {
    const dayData = ensureDay(day);
    const row = document.createElement("tr");
    setWeekendClass(row, day, dayData);

    const numberCell = document.createElement("td");
    numberCell.className = "day-number";
    numberCell.textContent = String(day);

    const date = new Date(state.selectedYear, state.selectedMonth, day);
    const dateCell = document.createElement("td");
    dateCell.className = "date-cell";
    dateCell.innerHTML = `${formatDate(state.selectedYear, state.selectedMonth, day)}<small>${WEEKDAYS[date.getDay()]}</small>`;

    const plateCell = document.createElement("td");
    plateCell.appendChild(createInput(day, "plate", dayData.plate, "plateSuggestions"));

    const placeCell = document.createElement("td");
    placeCell.appendChild(createInput(day, "place", dayData.place, "placeSuggestions"));

    const subjectCell = document.createElement("td");
    subjectCell.appendChild(createInput(day, "subject", dayData.subject, "subjectSuggestions"));

    const descriptionCell = document.createElement("td");
    descriptionCell.appendChild(createInput(day, "description", dayData.description));

    row.append(numberCell, dateCell, plateCell, placeCell, subjectCell, descriptionCell);
    entryBody.appendChild(row);
  }
}

function bindSignatureInput(input, property) {
  input.addEventListener("input", () => {
    ensureMonth()[property] = input.value;
    saveState();
  });

  input.addEventListener("blur", () => {
    const normalized = toTurkishUpper(input.value.trim());
    input.value = normalized;
    ensureMonth()[property] = normalized;

    if (property in state.settings) {
      state.settings[property] = normalized;
    }

    saveState();
  });
}

function renderSignatures() {
  const month = ensureMonth();

  preparedByInput.value = month.preparedBy || "";
  preparedTitleInput.value = month.preparedTitle || "";
  approvedByInput.value = month.approvedBy || "";
  approvedTitleInput.value = month.approvedTitle || "";

  const dateText = "……../…..…/" + state.selectedYear;
  document.getElementById("preparedDate").textContent = dateText;
  document.getElementById("approvedDate").textContent = dateText;
}

function collectSuggestions(field) {
  const values = new Set();

  Object.values(state.months).forEach((month) => {
    Object.values(month.days || {}).forEach((day) => {
      const value = String(day[field] || "").trim();
      if (value) values.add(value);
    });
  });

  return [...values].sort((a, b) => a.localeCompare(b, "tr"));
}

function fillDatalist(elementId, values) {
  const list = document.getElementById(elementId);
  list.innerHTML = "";

  values.forEach((value) => {
    const option = document.createElement("option");
    option.value = value;
    list.appendChild(option);
  });
}

function refreshSuggestions() {
  fillDatalist("plateSuggestions", collectSuggestions("plate"));
  fillDatalist("placeSuggestions", collectSuggestions("place"));
  fillDatalist("subjectSuggestions", collectSuggestions("subject"));
}

function render() {
  ensureMonth();
  buildYearOptions();
  renderMonthTabs();
  renderRows();
  renderSignatures();
  refreshSuggestions();
  periodLabel.textContent = `(${MONTHS[state.selectedMonth]}/${state.selectedYear})`;
  document.title = `${MONTHS[state.selectedMonth]} ${state.selectedYear} - Arazi Çıkış Cetveli`;
}

function clearSelectedMonth() {
  const label = `${MONTHS[state.selectedMonth]} ${state.selectedYear}`;
  const approved = window.confirm(`${label} ayındaki görev verileri silinsin mi? İsim alanları korunacaktır.`);

  if (!approved) return;

  ensureMonth().days = {};
  saveState();
  render();
}

function addYear() {
  const answer = window.prompt("Eklenecek yılı yazın:", String(state.selectedYear + 1));
  if (answer === null) return;

  const year = Number(answer);
  if (!Number.isInteger(year) || year < 2000 || year > 2100) {
    window.alert("2000 ile 2100 arasında geçerli bir yıl girin.");
    return;
  }

  if (!state.years.includes(year)) {
    state.years.push(year);
  }

  state.selectedYear = year;
  saveState();
  render();
}

async function downloadBackup() {
  try {
    await saveState(true);
    if (!desktopMode) throw new Error("SQLite servisi kapalı");

    const backupResponse = await fetch(apiUrl("/api/backup"), { method: "POST" });
    if (!backupResponse.ok) throw new Error(`HTTP ${backupResponse.status}`);
    const backupResult = await backupResponse.json();

    const databaseResponse = await fetch(apiUrl("/api/database"), { cache: "no-store" });
    if (!databaseResponse.ok) throw new Error(`HTTP ${databaseResponse.status}`);
    const blob = await databaseResponse.blob();
    const link = document.createElement("a");
    link.href = URL.createObjectURL(blob);
    link.download = `arazi-cikis-yedek-${new Date().toISOString().replace(/[:.]/g, "-").slice(0, 19)}.db`;
    link.click();
    URL.revokeObjectURL(link.href);

    window.alert(`SQLite yedeği oluşturuldu:
${backupResult.path}`);
  } catch (error) {
    console.error(error);
    window.alert("SQLite yedeği oluşturulamadı.");
  }
}

async function restoreSQLiteBackup(file) {
  const approved = window.confirm("Bu SQLite yedeği mevcut veritabanının yerine yüklensin mi?");
  if (!approved) return;

  const response = await fetch(apiUrl("/api/database"), {
    method: "POST",
    headers: { "Content-Type": "application/vnd.sqlite3" },
    body: await file.arrayBuffer()
  });
  if (!response.ok) {
    const detail = await response.text();
    throw new Error(detail || `HTTP ${response.status}`);
  }

  await loadStateFromDisk();
  render();
  window.alert("SQLite yedeği başarıyla yüklendi.");
}

function restoreLegacyJSON(file) {
  const reader = new FileReader();

  reader.onload = () => {
    try {
      const parsed = JSON.parse(String(reader.result));
      const restored = parsed.state || parsed;

      if (!restored.months || typeof restored.months !== "object") {
        throw new Error("Geçersiz yedek yapısı");
      }

      state = {
        selectedYear: Number(restored.selectedYear) || currentDate.getFullYear(),
        selectedMonth: Number.isInteger(restored.selectedMonth) ? restored.selectedMonth : 0,
        years: Array.isArray(restored.years) ? restored.years.map(Number) : [currentDate.getFullYear()],
        months: restored.months,
        settings: { ...DEFAULT_SETTINGS, ...(restored.settings || {}) }
      };

      saveState(true).then(() => {
        render();
        window.alert("Eski JSON yedeği SQLite veritabanına aktarıldı.");
      });
    } catch (error) {
      console.error(error);
      window.alert("JSON yedek dosyası okunamadı veya geçersiz.");
    }
  };

  reader.readAsText(file, "utf-8");
}

async function restoreBackup(file) {
  try {
    if (file.name.toLocaleLowerCase("tr-TR").endsWith(".json")) {
      restoreLegacyJSON(file);
      return;
    }
    await restoreSQLiteBackup(file);
  } catch (error) {
    console.error(error);
    window.alert("SQLite yedek dosyası okunamadı veya geçersiz.");
  }
}

yearSelect.addEventListener("change", () => {
  state.selectedYear = Number(yearSelect.value);
  saveState();
  render();
});

document.getElementById("addYearButton").addEventListener("click", addYear);
document.getElementById("printButton").addEventListener("click", () => window.print());
document.getElementById("clearMonthButton").addEventListener("click", clearSelectedMonth);
document.getElementById("backupButton").addEventListener("click", downloadBackup);
document.getElementById("restoreButton").addEventListener("click", () => {
  document.getElementById("restoreInput").click();
});
document.getElementById("openDataFolderButton").addEventListener("click", async () => {
  if (!desktopMode) {
    window.alert("Veri klasörü yalnızca masaüstü uygulamasında açılabilir.");
    return;
  }

  try {
    const response = await fetch(apiUrl("/api/open-data-folder"), { method: "POST" });
    if (!response.ok) throw new Error(`HTTP ${response.status}`);
  } catch (error) {
    console.error(error);
    window.alert("Veri klasörü açılamadı.");
  }
});
document.getElementById("closeAppButton").addEventListener("click", async () => {
  const approved = window.confirm("Son değişiklikler kaydedilip uygulama kapatılsın mı?");
  if (!approved) return;

  await saveState(true);
  if (desktopMode) {
    try {
      await fetch(apiUrl("/api/shutdown"), { method: "POST", keepalive: true });
    } catch {}
  }
  window.close();
});
document.getElementById("restoreInput").addEventListener("change", (event) => {
  const [file] = event.target.files;
  if (file) restoreBackup(file);
  event.target.value = "";
});

bindSignatureInput(preparedByInput, "preparedBy");
bindSignatureInput(preparedTitleInput, "preparedTitle");
bindSignatureInput(approvedByInput, "approvedBy");
bindSignatureInput(approvedTitleInput, "approvedTitle");

async function showFirstLaunchNotice() {
  if (!desktopMode || !firstLaunchNotice) return;

  try {
    const response = await fetch(apiUrl("/api/welcome"), { cache: "no-store" });
    if (!response.ok) throw new Error(`HTTP ${response.status}`);

    const payload = await response.json();
    if (!payload.show) return;

    firstLaunchNotice.hidden = false;
    requestAnimationFrame(() => firstLaunchNotice.classList.add("visible"));

    // Bildirim gösterildiği anda SQLite metadata tablosuna kaydedilir.
    // Böylece sonraki uygulama açılışlarında tekrar görünmez.
    fetch(apiUrl("/api/welcome"), { method: "POST", keepalive: true }).catch((error) => {
      console.error("İlk açılış bildirimi durumu kaydedilemedi:", error);
    });
  } catch (error) {
    console.error("İlk açılış bildirimi gösterilemedi:", error);
  }
}

function dismissFirstLaunchNotice() {
  if (!firstLaunchNotice) return;
  firstLaunchNotice.classList.remove("visible");
  window.setTimeout(() => {
    firstLaunchNotice.hidden = true;
  }, 180);
}

if (dismissWelcomeButton) {
  dismissWelcomeButton.addEventListener("click", dismissFirstLaunchNotice);
}

async function initializeApp() {
  saveStatus.textContent = "Veriler açılıyor...";
  await loadStateFromDisk();
  render();
  await showFirstLaunchNotice();
  saveStatus.textContent = desktopMode ? "SQLite veritabanına kaydedildi" : "SQLite bağlantı hatası";
}

window.addEventListener("beforeunload", () => {
  if (!desktopMode) return;
  const blob = new Blob([JSON.stringify(state)], { type: "application/json" });
  navigator.sendBeacon(apiUrl("/api/state"), blob);
});

initializeApp();

window.setInterval(() => {
  if (!desktopMode) return;
  fetch(apiUrl("/api/heartbeat"), { method: "POST", keepalive: true }).catch(() => {});
}, 30000);
