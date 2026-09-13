/* ============================================================
   Orkestra Control Center – controlcenter.js
   SSE live reload, theme toggle, mobile sidebar, filter/pagination
   ============================================================ */

(function () {
  'use strict';

  /* ── 1. Dark / Light theme toggle ─────────────────────────── */
  var THEME_KEY = 'cc-theme';

  function getTheme() {
    return localStorage.getItem(THEME_KEY) || 'dark';
  }

  function applyTheme(theme) {
    document.documentElement.setAttribute('data-theme', theme);
    localStorage.setItem(THEME_KEY, theme);
    var btn = document.getElementById('cc-theme-toggle');
    if (btn) {
      btn.title = theme === 'dark' ? 'Switch to light mode' : 'Switch to dark mode';
    }
  }

  // Apply theme immediately on load (also done inline in <head>)
  applyTheme(getTheme());

  var themeToggle = document.getElementById('cc-theme-toggle');
  if (themeToggle) {
    themeToggle.addEventListener('click', function () {
      var next = getTheme() === 'dark' ? 'light' : 'dark';
      applyTheme(next);
    });
  }

  /* ── 2. Sidebar: mobile toggle + collapsible ───────────────── */
  var mobileToggle = document.getElementById('cc-mobile-toggle');
  var sidebar = document.getElementById('cc-sidebar');
  var overlay = document.getElementById('cc-overlay');
  var collapseToggle = document.getElementById('cc-sidebar-toggle');

  // Restore collapsed state
  if (sidebar && localStorage.getItem('cc-sidebar-collapsed') === '1') {
    sidebar.classList.add('collapsed');
  }

  function openSidebar() {
    if (sidebar) sidebar.classList.add('open');
    if (overlay) overlay.classList.add('open');
  }

  function closeSidebar() {
    if (sidebar) sidebar.classList.remove('open');
    if (overlay) overlay.classList.remove('open');
  }

  if (mobileToggle) {
    mobileToggle.addEventListener('click', function () {
      if (sidebar && sidebar.classList.contains('open')) {
        closeSidebar();
      } else {
        openSidebar();
      }
    });
  }

  if (overlay) {
    overlay.addEventListener('click', closeSidebar);
  }

  // Desktop collapse toggle — sync tooltips on nav items
  function syncNavTooltips(isCollapsed) {
    document.querySelectorAll('.cc-nav-item').forEach(function (item) {
      if (isCollapsed) {
        // Extract text content (strip svg text)
        var text = '';
        item.childNodes.forEach(function (n) {
          if (n.nodeType === Node.TEXT_NODE) text += n.textContent.trim();
        });
        if (text) item.setAttribute('title', text);
      } else {
        item.removeAttribute('title');
      }
    });
  }

  // Apply tooltip state on load
  if (sidebar) syncNavTooltips(sidebar.classList.contains('collapsed'));

  if (collapseToggle && sidebar) {
    collapseToggle.addEventListener('click', function () {
      var collapsed = sidebar.classList.toggle('collapsed');
      localStorage.setItem('cc-sidebar-collapsed', collapsed ? '1' : '0');
      syncNavTooltips(collapsed);
    });
  }

  /* ── 2b. Grid / List view toggle ──────────────────────────── */
  var VIEW_KEY = 'cc-view';
  var cardGrid = document.getElementById('crdGrid') || document.getElementById('crCardGrid');
  var viewBtns = document.querySelectorAll('.cc-view-btn');

  function setView(view) {
    if (cardGrid) {
      if (view === 'list') {
        cardGrid.classList.add('list-view');
      } else {
        cardGrid.classList.remove('list-view');
      }
    }
    viewBtns.forEach(function (btn) {
      btn.classList.toggle('active', btn.getAttribute('data-view') === view);
    });
    localStorage.setItem(VIEW_KEY, view);
  }

  // Read page default from whichever toggle button has 'active' in the HTML
  var pageDefaultView = 'grid';
  viewBtns.forEach(function (btn) {
    if (btn.classList.contains('active')) {
      pageDefaultView = btn.getAttribute('data-view') || 'grid';
    }
  });
  // Restore saved view, falling back to page's own default
  var savedView = localStorage.getItem(VIEW_KEY) || pageDefaultView;
  setView(savedView);

  viewBtns.forEach(function (btn) {
    btn.addEventListener('click', function () {
      setView(btn.getAttribute('data-view'));
    });
  });

  /* ── 3. SSE live connection + partial DOM updates ──────────── */
  var sseDot = document.getElementById('sse-dot');
  var sseRetryDelay = 1000;
  var sseMaxRetry = 30000;
  var sseSource = null;
  var sseDisconnectedBanner = null;
  // Only show the disconnected banner after this many consecutive failures
  var SSE_BANNER_THRESHOLD = 4;
  var sseFailCount = 0;

  function setSseConnected(connected) {
    if (connected) {
      // Reset failure counter on successful connection
      sseFailCount = 0;
    }

    if (sseDot) {
      if (connected) {
        sseDot.classList.add('connected');
      } else {
        sseDot.classList.remove('connected');
      }
    }

    // Show disconnected banner only after several consecutive failures
    if (!connected) {
      sseFailCount++;
      if (sseFailCount >= SSE_BANNER_THRESHOLD && !sseDisconnectedBanner) {
        sseDisconnectedBanner = document.createElement('div');
        sseDisconnectedBanner.id = 'cc-disconnected-banner';
        sseDisconnectedBanner.className = 'cc-alert cc-alert-warn cc-disconnected-banner';
        sseDisconnectedBanner.innerHTML =
          '<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/><line x1="12" y1="8" x2="12" y2="12"/><line x1="12" y1="16" x2="12.01" y2="16"/></svg>' +
          '<span>Disconnected from runtime — data may be stale</span>';
        var page = document.querySelector('.cc-page');
        if (page) page.insertBefore(sseDisconnectedBanner, page.firstChild);
      }
    } else {
      if (sseDisconnectedBanner) {
        sseDisconnectedBanner.remove();
        sseDisconnectedBanner = null;
      }
    }
  }

  /* Partial DOM update — fetch snapshot JSON and update elements in-place */
  function setText(el, val) {
    if (el && el.textContent !== String(val)) el.textContent = val;
  }

  function fetchAndApplyUpdate() {
    fetch('/controlcenter/api/snapshot', { cache: 'no-store' })
      .then(function (r) {
        if (!r.ok) throw new Error('snapshot ' + r.status);
        return r.json();
      })
      .then(function (data) {
        applySnapshot(data);
      })
      .catch(function (err) {
        console.warn('CC live update failed:', err);
      });
  }

  function applySnapshot(data) {
    var s = data.stats;
    if (!s) return;

    // ── Global stats (index page) ──
    setText(document.querySelector('[data-live="totalKatalogs"]'), s.totalKatalogs);
    setText(document.querySelector('[data-live="totalCRDs"]'), s.totalCRDs);
    setText(document.querySelector('[data-live="totalWorkers"]'), s.totalWorkers);
    setText(document.querySelector('[data-live="totalResources"]'), s.totalResources);
    var healthyEl = document.querySelector('[data-live="healthyKatalogs"]');
    if (healthyEl) setText(healthyEl, s.healthyKatalogs + ' healthy');

    // ── Per-katalog cards (index page) ──
    if (data.katalogs && data.katalogs.length) {
      data.katalogs.forEach(function (kat) {
        var card = document.querySelector('[data-katalog="' + CSS.escape(kat.name) + '"]');
        if (!card) return;

        // Badge
        var badge = card.querySelector('[data-live="badge"]');
        if (badge) {
          var healthy = kat.healthy;
          badge.className = 'cc-badge ' + (healthy ? 'cc-badge-healthy' : 'cc-badge-degraded');
          badge.innerHTML = healthy
            ? '<span class="cc-status-dot healthy"></span>Healthy'
            : '<span class="cc-status-dot degraded"></span>Degraded';
        }

        // Meta values
        var crdEl = card.querySelector('[data-live="totalCRDs"]');
        if (crdEl) {
          crdEl.innerHTML = kat.totalCRDs + ' <span class="text-muted">(' + kat.healthyCRDs + ' healthy)</span>';
        }
        setText(card.querySelector('[data-live="totalWorkers"]'), kat.totalWorkers);
        setText(card.querySelector('[data-live="totalResources"]'), kat.totalResources);
      });
    }
  }

  function connectSSE() {
    if (sseSource) {
      sseSource.close();
      sseSource = null;
    }

    try {
      sseSource = new EventSource('/controlcenter/sse');
    } catch (e) {
      setSseConnected(false);
      scheduleReconnect();
      return;
    }

    sseSource.onopen = function () {
      setSseConnected(true);
      sseRetryDelay = 1000;
    };

    sseSource.onmessage = function (e) {
      if (e.data === 'reload' || e.data === 'update' || e.data === 'connected') {
        if (e.data !== 'connected') {
          fetchAndApplyUpdate();
        }
      }
    };

    sseSource.onerror = function () {
      setSseConnected(false);
      sseSource.close();
      sseSource = null;
      scheduleReconnect();
    };
  }

  function scheduleReconnect() {
    setTimeout(function () {
      connectSSE();
    }, sseRetryDelay);
    sseRetryDelay = Math.min(sseRetryDelay * 2, sseMaxRetry);
  }

  connectSSE();

  /* ── 4. CRD filter + search (katalog page) ─────────────────── */
  var PAGE_SIZE = 12;
  var currentFilter = 'all';
  var currentSearch = '';
  var currentPage = 1;
  var allCards = [];

  function getAllCards() {
    return Array.from(document.querySelectorAll('.crd-grid-item'));
  }

  function filterAndPaginate() {
    allCards = getAllCards();
    if (allCards.length === 0) return;

    allCards.forEach(function (card) {
      var name = (card.dataset.name || '').toLowerCase();
      var status = card.dataset.status || '';

      var matchesFilter =
        currentFilter === 'all' ||
        (currentFilter === 'healthy' && status === 'healthy') ||
        (currentFilter === 'started' && status === 'started') ||
        (currentFilter === 'pending' && status === 'pending') ||
        (currentFilter === 'degraded' && status === 'degraded');

      var matchesSearch = !currentSearch || name.includes(currentSearch);

      if (matchesFilter && matchesSearch) {
        card.classList.remove('hide');
      } else {
        card.classList.add('hide');
      }
    });

    var visible = allCards.filter(function (c) { return !c.classList.contains('hide'); });
    var totalPages = Math.ceil(visible.length / PAGE_SIZE);

    if (currentPage > totalPages && totalPages > 0) currentPage = totalPages;
    if (currentPage < 1) currentPage = 1;

    allCards.forEach(function (c) { c.classList.add('hide-for-pagination'); });

    var start = (currentPage - 1) * PAGE_SIZE;
    var end = Math.min(start + PAGE_SIZE, visible.length);

    for (var i = start; i < end; i++) {
      if (visible[i]) visible[i].classList.remove('hide-for-pagination');
    }

    // Update pagination info
    var ps = document.getElementById('pageStart');
    var pe = document.getElementById('pageEnd');
    var tc = document.getElementById('totalCount');
    if (ps) ps.textContent = visible.length === 0 ? 0 : start + 1;
    if (pe) pe.textContent = Math.min(end, visible.length);
    if (tc) tc.textContent = visible.length;

    var prevBtn = document.getElementById('prevPage');
    var nextBtn = document.getElementById('nextPage');
    if (prevBtn) prevBtn.disabled = currentPage <= 1;
    if (nextBtn) nextBtn.disabled = currentPage >= totalPages;

    generatePageNumbers(currentPage, totalPages);

    var emptyState = document.getElementById('emptyState');
    if (emptyState) {
      if (visible.length === 0 && allCards.length > 0) {
        emptyState.classList.remove('hidden');
      } else {
        emptyState.classList.add('hidden');
      }
    }
  }

  function generatePageNumbers(curPage, total) {
    var container = document.getElementById('pageNumbers');
    if (!container) return;
    container.innerHTML = '';
    if (total <= 1) return;

    var startP = Math.max(1, curPage - 2);
    var endP = Math.min(total, startP + 4);
    if (endP - startP < 4) startP = Math.max(1, endP - 4);

    for (var i = startP; i <= endP; i++) {
      var btn = document.createElement('button');
      btn.textContent = i;
      btn.className = 'cc-page-btn' + (i === curPage ? ' active' : '');
      (function (page) {
        btn.addEventListener('click', function () {
          currentPage = page;
          filterAndPaginate();
        });
      })(i);
      container.appendChild(btn);
    }
  }

  // Filter buttons (CRD page uses .cc-filter-btn[data-filter])
  document.querySelectorAll('.cc-filter-btn[data-filter]').forEach(function (btn) {
    btn.addEventListener('click', function () {
      document.querySelectorAll('.cc-filter-btn[data-filter]').forEach(function (b) {
        b.classList.remove('active');
      });
      btn.classList.add('active');
      currentFilter = btn.dataset.filter;
      currentPage = 1;
      filterAndPaginate();
    });
  });

  // Search
  var searchInput = document.getElementById('searchInput');
  if (searchInput) {
    searchInput.addEventListener('input', function (e) {
      currentSearch = e.target.value.toLowerCase();
      currentPage = 1;
      filterAndPaginate();
    });
  }

  var prevBtn = document.getElementById('prevPage');
  if (prevBtn) {
    prevBtn.addEventListener('click', function () {
      if (currentPage > 1) { currentPage--; filterAndPaginate(); }
    });
  }

  var nextBtn = document.getElementById('nextPage');
  if (nextBtn) {
    nextBtn.addEventListener('click', function () {
      var vis = allCards.filter(function (c) { return !c.classList.contains('hide'); });
      var total = Math.ceil(vis.length / PAGE_SIZE);
      if (currentPage < total) { currentPage++; filterAndPaginate(); }
    });
  }

  // Initialize CRD filter/pagination
  setTimeout(function () {
    allCards = getAllCards();
    if (allCards.length > 0) filterAndPaginate();
  }, 50);

  /* ── 5. Resource list filter + pagination (cr_list page) ───── */
  var CR_PAGE_SIZE = 20;
  var crFilter = 'all';
  var crSearch = '';
  var crPage = 1;
  var allRows = [];

  function getAllRows() {
    return Array.from(document.querySelectorAll('#resourceTableBody .resource-row'));
  }

  function filterRows() {
    allRows = getAllRows();
    if (allRows.length === 0) return;

    allRows.forEach(function (row) {
      var name = row.dataset.name || '';
      var namespace = row.dataset.namespace || '';
      var phase = row.dataset.phase || '';
      var ready = row.dataset.ready === 'true';

      var matchesFilter =
        crFilter === 'all' ||
        (crFilter === 'ready' && ready) ||
        (crFilter === 'not-ready' && !ready) ||
        (crFilter === 'succeeded' && phase === 'succeeded') ||
        (crFilter === 'failed' && phase === 'failed') ||
        (crFilter === 'running' && phase.includes('running')) ||
        (crFilter === 'pending' && phase === 'pending');

      var matchesSearch = !crSearch || name.includes(crSearch) || namespace.includes(crSearch);

      if (matchesFilter && matchesSearch) {
        row.classList.remove('filtered-out');
      } else {
        row.classList.add('filtered-out');
      }
    });

    var visible = allRows.filter(function (r) { return !r.classList.contains('filtered-out'); });
    updateCrPagination(visible);

    var empty = document.getElementById('emptyFilterState');
    if (empty) {
      if (visible.length === 0 && allRows.length > 0) {
        empty.classList.remove('hidden');
      } else {
        empty.classList.add('hidden');
      }
    }
  }

  function updateCrPagination(visible) {
    var total = visible.length;
    var totalPages = Math.ceil(total / CR_PAGE_SIZE);
    if (crPage > totalPages && totalPages > 0) crPage = totalPages;
    if (crPage < 1) crPage = 1;

    allRows.forEach(function (r) { r.style.display = 'none'; });

    var start = (crPage - 1) * CR_PAGE_SIZE;
    var end = start + CR_PAGE_SIZE;
    visible.slice(start, end).forEach(function (r) { r.style.display = ''; });

    var ps = document.getElementById('pageStart');
    var pe = document.getElementById('pageEnd');
    var tc = document.getElementById('totalCount');
    if (ps) ps.textContent = total === 0 ? 0 : start + 1;
    if (pe) pe.textContent = Math.min(end, total);
    if (tc) tc.textContent = total;

    var prevB = document.getElementById('prevPage');
    var nextB = document.getElementById('nextPage');
    if (prevB) prevB.disabled = crPage <= 1;
    if (nextB) nextB.disabled = crPage >= totalPages;

    generateCrPageNumbers(crPage, totalPages);
  }

  function generateCrPageNumbers(cur, total) {
    var container = document.getElementById('pageNumbers');
    if (!container) return;
    container.innerHTML = '';
    if (total <= 1) return;

    var start = Math.max(1, cur - 2);
    var end = Math.min(total, start + 4);
    if (end - start < 4) start = Math.max(1, end - 4);

    for (var i = start; i <= end; i++) {
      var btn = document.createElement('button');
      btn.textContent = i;
      btn.className = 'cc-page-btn' + (i === cur ? ' active' : '');
      (function (page) {
        btn.addEventListener('click', function () {
          crPage = page;
          filterRows();
        });
      })(i);
      container.appendChild(btn);
    }
  }

  // Hook up cr_list filter buttons
  document.querySelectorAll('.cr-filter-btn[data-filter]').forEach(function (btn) {
    btn.addEventListener('click', function () {
      document.querySelectorAll('.cr-filter-btn[data-filter]').forEach(function (b) {
        b.classList.remove('active');
      });
      btn.classList.add('active');
      crFilter = btn.dataset.filter;
      crPage = 1;
      filterRows();
    });
  });

  var crSearchInput = document.getElementById('crSearchInput');
  if (crSearchInput) {
    crSearchInput.addEventListener('input', function (e) {
      crSearch = e.target.value.toLowerCase();
      crPage = 1;
      filterRows();
    });
  }

  setTimeout(function () {
    allRows = getAllRows();
    if (allRows.length > 0) filterRows();
  }, 50);

  /* ── 6. Event pagination (cr_detail page) ─────────────────── */
  var EVENT_PAGE_SIZE = 10;
  var eventPage = 1;
  var eventRows = [];

  function initEventPagination() {
    eventRows = Array.from(document.querySelectorAll('#eventsTableBody .event-row'));
    if (eventRows.length === 0) return;
    updateEventPage();
  }

  function updateEventPage() {
    var total = eventRows.length;
    var totalPages = Math.ceil(total / EVENT_PAGE_SIZE);
    if (eventPage > totalPages) eventPage = totalPages;
    if (eventPage < 1) eventPage = 1;

    eventRows.forEach(function (r) { r.style.display = 'none'; });
    var start = (eventPage - 1) * EVENT_PAGE_SIZE;
    var end = Math.min(start + EVENT_PAGE_SIZE, total);
    for (var i = start; i < end; i++) {
      if (eventRows[i]) eventRows[i].style.display = '';
    }

    var es = document.getElementById('eventStart');
    var ee = document.getElementById('eventEnd');
    var et = document.getElementById('eventTotalCount');
    if (es) es.textContent = start + 1;
    if (ee) ee.textContent = end;
    if (et) et.textContent = total;

    var prevB = document.getElementById('eventPrevPageBtn');
    var nextB = document.getElementById('eventNextPageBtn');
    if (prevB) prevB.disabled = eventPage <= 1;
    if (nextB) nextB.disabled = eventPage >= totalPages;

    generateEventPageNumbers(eventPage, totalPages);
  }

  function generateEventPageNumbers(cur, total) {
    var container = document.getElementById('eventPageNumbers');
    if (!container) return;
    container.innerHTML = '';
    if (total <= 1) return;

    var start = Math.max(1, cur - 2);
    var end = Math.min(total, start + 4);
    if (end - start < 4) start = Math.max(1, end - 4);

    for (var i = start; i <= end; i++) {
      var btn = document.createElement('button');
      btn.textContent = i;
      btn.className = 'cc-page-btn' + (i === cur ? ' active' : '');
      (function (page) {
        btn.addEventListener('click', function () {
          eventPage = page;
          updateEventPage();
        });
      })(i);
      container.appendChild(btn);
    }
  }

  var epPrev = document.getElementById('eventPrevPageBtn');
  var epNext = document.getElementById('eventNextPageBtn');
  if (epPrev) {
    epPrev.addEventListener('click', function () {
      if (eventPage > 1) { eventPage--; updateEventPage(); }
    });
  }
  if (epNext) {
    epNext.addEventListener('click', function () {
      var total = Math.ceil(eventRows.length / EVENT_PAGE_SIZE);
      if (eventPage < total) { eventPage++; updateEventPage(); }
    });
  }

  setTimeout(initEventPagination, 50);

  /* ── 7. Worker expand/collapse (crd detail page) ───────────── */
  var toggleBtn = document.getElementById('toggleWorkersBtn');
  if (toggleBtn) {
    var moreWorkers = document.getElementById('moreWorkers');
    var toggleIcon = document.getElementById('toggleIcon');
    var expanded = false;

    toggleBtn.addEventListener('click', function () {
      expanded = !expanded;
      if (moreWorkers) {
        if (expanded) {
          moreWorkers.classList.remove('hidden');
        } else {
          moreWorkers.classList.add('hidden');
        }
      }
      if (toggleIcon) {
        toggleIcon.style.transform = expanded ? 'rotate(180deg)' : '';
      }
    });
  }

  /* ── Row click navigation via data-href ────────────────────── */
  document.querySelectorAll('[data-href]').forEach(function (row) {
    row.style.cursor = 'pointer';
    row.addEventListener('click', function () {
      window.location.href = row.getAttribute('data-href');
    });
  });

})();
