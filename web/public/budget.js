// ---- Weekly performance chart ----

function initBudgetPerfChart() {
    const canvas = document.getElementById('budget-perf-chart');
    if (!canvas) return;
    const existing = Chart.getChart(canvas);
    if (existing) existing.destroy();
    const actuals = JSON.parse(canvas.dataset.actuals);
    const target  = parseFloat(canvas.dataset.target);
    const labels  = JSON.parse(canvas.dataset.labels);
    new Chart(canvas, {
        data: {
            labels,
            datasets: [{
                type: 'bar',
                label: 'Weekly Spend',
                data: actuals,
                backgroundColor: actuals.map(v =>
                    target > 0 && v > target ? 'rgba(220,53,69,0.75)' : 'rgba(25,135,84,0.75)'),
                borderColor: actuals.map(v =>
                    target > 0 && v > target ? 'rgb(220,53,69)' : 'rgb(25,135,84)'),
                borderWidth: 1,
                order: 2,
            }, {
                type: 'line',
                label: 'Budget Target',
                data: Array(actuals.length).fill(target),
                borderColor: 'rgba(0,0,0,0.3)',
                borderDash: [6, 4],
                borderWidth: 2,
                pointRadius: 0,
                fill: false,
                order: 1,
            }]
        },
        options: {
            responsive: true,
            maintainAspectRatio: false,
            interaction: { mode: 'index' },
            scales: {
                y: {
                    beginAtZero: true,
                    ticks: { callback: v => '$' + v.toLocaleString() }
                }
            },
            plugins: {
                legend: { position: 'top' },
                tooltip: {
                    callbacks: {
                        label: ctx => ' ' + ctx.dataset.label + ': $' + ctx.parsed.y.toFixed(2)
                    }
                }
            }
        }
    });
}

document.body.addEventListener('htmx:afterSwap', initBudgetPerfChart);
initBudgetPerfChart();

// ---- Broad category collapse ----
//
// Collapse state lives here rather than in the DOM because saving a target
// swaps that row's <tbody> out, which would otherwise drop the collapsed class
// and pop a single row back open inside a collapsed group.

const collapsedGroups = new Set();

function applyGroupCollapse() {
    document.querySelectorAll('tbody[data-group]').forEach(tbody => {
        tbody.classList.toggle('d-none', collapsedGroups.has(tbody.dataset.group));
    });
}

function toggleBroadGroup(btn, groupId) {
    const nowCollapsed = !collapsedGroups.has(groupId);
    if (nowCollapsed) {
        collapsedGroups.add(groupId);
    } else {
        collapsedGroups.delete(groupId);
    }
    applyGroupCollapse();
    btn.setAttribute('aria-expanded', String(!nowCollapsed));
    const icon = btn.querySelector('i');
    if (icon) icon.className = nowCollapsed ? 'bi bi-folder2' : 'bi bi-folder2-open';
}

document.body.addEventListener('htmx:afterSwap', applyGroupCollapse);

// ---- Merchant tag popovers ----
//
// The table shows the highest-spending merchants inline and folds the rest
// into a popover. Instances are tracked so that rows swapped out by htmx do
// not leave orphaned popovers attached to the body.

const merchantPopovers = new Map(); // trigger element -> bootstrap.Popover

function initMerchantPopovers() {
    merchantPopovers.forEach((popover, el) => {
        if (!document.body.contains(el)) {
            popover.dispose();
            merchantPopovers.delete(el);
        }
    });

    document.querySelectorAll('.budge-merchants-more').forEach(btn => {
        if (merchantPopovers.has(btn)) return;
        const source = btn.parentElement.querySelector('[data-merchant-list]');
        if (!source) return;
        merchantPopovers.set(btn, new bootstrap.Popover(btn, {
            html: true,
            // 'focus' gives click to open, click-away to dismiss.
            trigger: 'focus',
            placement: 'top',
            container: 'body',
            title: 'Merchants',
            // Escaped server-side by html/template.
            content: () => source.innerHTML,
        }));
    });
}

document.body.addEventListener('htmx:afterSwap', initMerchantPopovers);
initMerchantPopovers();

// ---- Save confirmation ----

function flashSaved(scope) {
    const badge = scope.querySelector('[data-saved-flash]');
    if (!badge) return;
    badge.classList.add('budge-flash-show');
    clearTimeout(badge._flashTimer);
    badge._flashTimer = setTimeout(() => badge.classList.remove('budge-flash-show'), 1500);
}

// ---- Target frequency conversion ----

function _toWeekly(v, f) {
    if (f === 'fortnightly') return v / 2;
    if (f === 'monthly')     return v * 12 / 52;
    if (f === 'yearly')      return v / 52;
    return v;
}
function _fromWeekly(v, f) {
    if (f === 'fortnightly') return v * 2;
    if (f === 'monthly')     return v * 52 / 12;
    if (f === 'yearly')      return v * 52;
    return v;
}
function convertTargetFrequency(sel) {
    const prev = sel.dataset.prev || 'weekly';
    const next = sel.value;
    if (prev === next) return;
    const amountEl = sel.closest('tr').querySelector('input[name="target_amount"]');
    if (amountEl && amountEl.value !== '') {
        const weekly = _toWeekly(parseFloat(amountEl.value), prev);
        amountEl.value = _fromWeekly(weekly, next).toFixed(2);
    }
    sel.dataset.prev = next;
}
