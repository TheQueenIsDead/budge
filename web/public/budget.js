// ---- Weekly performance chart ----

function initBudgetPerfChart() {
    // A chart whose canvas has been swapped out is still registered, and
    // still listening for resizes, until it is destroyed.
    Object.values(Chart.instances).forEach(chart => {
        if (!chart.canvas.isConnected) chart.destroy();
    });
    const canvas = document.getElementById('budget-perf-chart');
    if (!canvas) return;
    const existing = Chart.getChart(canvas);
    if (existing) existing.destroy();
    const actuals = JSON.parse(canvas.dataset.actuals);
    const target  = parseFloat(canvas.dataset.target);
    const labels  = JSON.parse(canvas.dataset.labels);
    const goal    = parseFloat(canvas.dataset.savings) || 0;
    const monthly = canvas.dataset.monthly === 'true';
    const moneyIn = JSON.parse(canvas.dataset.moneyIn || '[]');
    const income  = actuals.map((_, i) => moneyIn[i] || 0);
    const hasIncome = income.some(v => v > 0);

    // Each period split against the budget and against what actually came
    // in: pay, people paying you back, money from accounts Budge no longer
    // tracks. Spending up to the budget target is blue, past the target but
    // still within what came in is amber, and past what came in is red on
    // top. What was left over is green up to the savings goal, grey beyond.
    // The last bar is the week or month in progress: what it has not spent
    // yet is not saved yet either, and its pay may not have landed, so it is
    // only measured against the budget.
    const current = actuals.length - 1;
    const cap = actuals.map((_, i) => hasIncome && i !== current ? income[i] : Infinity);
    const budget = target > 0 ? target : Infinity;
    const within     = actuals.map((v, i) => Math.min(v, budget, cap[i]));
    const overBudget = actuals.map((v, i) => Math.max(Math.min(v, cap[i]) - within[i], 0));
    const overspent  = actuals.map((v, i) => Math.max(v - cap[i], 0));
    const saved       = actuals.map((v, i) => i === current ? 0 : Math.min(Math.max(income[i] - v, 0), goal));
    const unallocated = actuals.map((v, i) => i === current ? 0 : Math.max(income[i] - v - saved[i], 0));
    if (current >= 0) labels[current] += ' (so far)';

    const money = v => '$' + v.toLocaleString(undefined, {minimumFractionDigits: 2, maximumFractionDigits: 2});
    const bar = (label, data, colour) => ({
        type: 'bar', label, data, stack: 'week',
        backgroundColor: colour, borderWidth: 0, order: 2,
    });
    const datasets = [
        bar(target > 0 ? 'Within budget' : 'Spending', within, 'rgba(13,110,253,0.7)'),
        bar('Over budget', overBudget, 'rgba(253,153,47,0.85)'),
    ];
    if (hasIncome) {
        datasets.push(
            bar('Overspent', overspent, 'rgba(220,53,69,0.8)'),
            bar('Savings', saved, 'rgba(25,135,84,0.75)'),
            bar('Unallocated', unallocated, 'rgba(108,117,125,0.35)'),
        );
    }
    // What came in is the line spending has to stay under: dotted green, to
    // tell it from the budget target's grey dashes. Months step with what
    // each one brought in; weeks are level at the average week, since pay
    // lands once a month. The month in progress is left off, its pay may
    // not have landed yet.
    if (hasIncome) {
        datasets.push({
            type: 'line',
            label: monthly ? 'Money in' : 'Money in (weekly average)',
            // Lines get a stack each: the y axis stacks the bars, and a line
            // left in the default stack is drawn on top of the one before it.
            stack: 'money-in',
            data: income.map((v, i) => monthly && i === current ? null : v),
            stepped: monthly ? 'middle' : false,
            borderColor: 'rgba(25,135,84,0.9)',
            borderDash: [2, 4],
            borderCapStyle: 'round',
            borderWidth: 2.5,
            pointRadius: 0,
            fill: false,
            order: 0,
        });
    }
    if (target > 0) {
        datasets.push({
            type: 'line',
            label: 'Budget Target',
            stack: 'target',
            data: Array(actuals.length).fill(target),
            borderColor: 'rgba(0,0,0,0.35)',
            borderDash: [6, 4],
            borderWidth: 2,
            pointRadius: 0,
            fill: false,
            order: 1,
        });
    }

    new Chart(canvas, {
        data: {labels, datasets},
        options: {
            responsive: true,
            maintainAspectRatio: false,
            interaction: { mode: 'index' },
            scales: {
                x: { stacked: true },
                y: {
                    stacked: true,
                    beginAtZero: true,
                    ticks: { callback: v => '$' + v.toLocaleString() }
                }
            },
            plugins: {
                legend: { position: 'top' },
                tooltip: {
                    // Empty segments are left out, so a good week does not
                    // list "Overspent: $0.00".
                    filter: item => item.parsed.y > 0,
                    callbacks: {
                        label: ctx => ' ' + ctx.dataset.label + ': ' + money(ctx.parsed.y),
                        footer: items => 'Total spent: ' + money(actuals[items[0].dataIndex]),
                    }
                }
            }
        }
    });
}

// After settle, not swap. The canvas has an id, so htmx settles it: the new
// canvas briefly carries the old one's attributes, and gets its own back
// once settled. A chart drawn on swap was sized before that and then had
// its width and height taken from under it, leaving it squashed into the
// corner after a range change.
document.body.addEventListener('htmx:afterSettle', initBudgetPerfChart);
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
