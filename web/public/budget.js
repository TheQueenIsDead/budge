// ---- Weekly performance chart ----

// budgetedColours fills each week's budgeted segment the way the dashboard's
// spark bars are drawn: a pastel tint for every week, sage when it came in at
// or under target and clay when it went over, and the full accent for the week
// open in the breakdown, so the bar and the table are visibly the same week.
function budgetedColours(actuals, target, weeks) {
    const selected = document.getElementById('budget-week-start')?.value;
    return actuals.map((v, i) => {
        const accent = target > 0 && v > target ? 'clay' : 'sage';
        return weeks[i] === selected ? budge.colour(accent) : budge.colourSoft(accent);
    });
}

function initBudgetPerfChart() {
    const canvas = document.getElementById('budget-perf-chart');
    if (!canvas) return;
    const existing = Chart.getChart(canvas);
    if (existing) existing.destroy();
    const actuals = JSON.parse(canvas.dataset.actuals);
    const series  = JSON.parse(canvas.dataset.series || '[]');
    const target  = parseFloat(canvas.dataset.target);
    const labels  = JSON.parse(canvas.dataset.labels);
    const weeks   = JSON.parse(canvas.dataset.weeks || '[]');
    const money   = v => '$' + v.toLocaleString(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 2 });

    // One stacked bar per week: budgeted spend at the base, unbudgeted on top,
    // so whatever rises past the target line is visibly the unbudgeted part.
    // The server always sends both in that order. The unbudgeted part is left
    // unfilled and outlined in grey, so it reads as spend the budget never
    // planned for.
    const under = budge.colourSoft('sage');
    const over = budge.colourSoft('clay');
    const unbudgeted = budge.colour('muted');
    const [budgetedSeries, unbudgetedSeries] = series;
    const datasets = [];
    if (budgetedSeries) {
        datasets.push({
            type: 'bar',
            label: budgetedSeries.label,
            data: budgetedSeries.data,
            stack: 'spend',
            backgroundColor: budgetedColours(actuals, target, weeks),
            // A surface-coloured top edge leaves a gap before the outline above.
            borderColor: budge.surface,
            borderWidth: { top: 2 },
            borderSkipped: 'bottom',
            order: 2,
        });
    }
    if (unbudgetedSeries) {
        datasets.push({
            type: 'bar',
            label: unbudgetedSeries.label,
            data: unbudgetedSeries.data,
            stack: 'spend',
            backgroundColor: 'transparent',
            hoverBackgroundColor: 'transparent',
            borderColor: unbudgeted,
            borderWidth: 2,
            borderSkipped: false,
            order: 2,
        });
    }
    datasets.push({
        type: 'line',
        label: 'Budget Target',
        data: Array(labels.length).fill(target),
        borderColor: budge.ink,
        borderDash: [6, 4],
        borderWidth: 2,
        pointRadius: 0,
        fill: false,
        order: 1,
    });

    new Chart(canvas, {
        data: { labels, datasets },
        options: {
            responsive: true,
            maintainAspectRatio: false,
            interaction: { mode: 'index', intersect: false },
            // A bar opens that week in the breakdown beneath the chart.
            onClick: (event, elements) => {
                if (!elements.length || !weeks[elements[0].index]) return;
                htmx.ajax('GET', '/budget/week?start=' + weeks[elements[0].index],
                    { target: '#budget-week', swap: 'innerHTML' });
            },
            onHover: (event, elements) => {
                event.native.target.style.cursor = elements.length ? 'pointer' : 'default';
            },
            scales: {
                x: { stacked: true, grid: { display: false }, ticks: { color: budge.ink } },
                y: {
                    stacked: true,
                    beginAtZero: true,
                    grid: { color: budge.grid },
                    ticks: { color: budge.ink, callback: v => '$' + v.toLocaleString() }
                }
            },
            plugins: {
                // The budgeted colour varies by week, so the legend names what
                // each colour means rather than listing the datasets.
                legend: {
                    position: 'top',
                    onClick: null,
                    labels: {
                        color: budge.ink,
                        boxWidth: 12,
                        generateLabels: () => [
                            { text: 'Budgeted, under target', fillStyle: under, strokeStyle: budge.colour('sage'), lineWidth: 1, fontColor: budge.ink },
                            { text: 'Budgeted, over target', fillStyle: over, strokeStyle: budge.colour('clay'), lineWidth: 1, fontColor: budge.ink },
                            { text: 'Unbudgeted', fillStyle: 'transparent', strokeStyle: unbudgeted, lineWidth: 2, fontColor: budge.ink },
                            { text: 'Budget target', fillStyle: 'transparent', strokeStyle: budge.ink, lineWidth: 2, lineDash: [6, 4], fontColor: budge.ink },
                        ],
                    },
                },
                tooltip: {
                    // Empty segments are noise in a per-week list.
                    filter: item => item.dataset.type === 'line' || item.parsed.y > 0,
                    itemSort: (a, b) => (a.dataset.type === 'line') - (b.dataset.type === 'line') || b.parsed.y - a.parsed.y,
                    callbacks: {
                        label: ctx => ' ' + ctx.dataset.label + ': ' + money(ctx.parsed.y),
                        footer: items => {
                            const i = items[0].dataIndex;
                            const diff = actuals[i] - target;
                            const lines = ['Total: ' + money(actuals[i])];
                            if (target > 0) lines.push((diff > 0 ? 'Over target by ' : 'Under target by ') + money(Math.abs(diff)));
                            lines.push('Click for the breakdown');
                            return lines;
                        }
                    }
                }
            }
        }
    });
}

// A new breakdown moves the highlight, which only needs the colours redrawn;
// anything else swapped in may be the chart itself, so that is rebuilt.
document.body.addEventListener('htmx:afterSwap', event => {
    const canvas = document.getElementById('budget-perf-chart');
    const chart = canvas && Chart.getChart(canvas);
    if (chart && event.detail.target.id === 'budget-week') {
        const bars = chart.data.datasets[0];
        bars.backgroundColor = budgetedColours(
            JSON.parse(canvas.dataset.actuals), parseFloat(canvas.dataset.target), JSON.parse(canvas.dataset.weeks || '[]'));
        chart.update('none');
        return;
    }
    initBudgetPerfChart();
});
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
