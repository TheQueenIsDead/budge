// ---- Weekly performance chart ----

function initBudgetPerfChart() {
    const canvas = document.getElementById('budget-perf-chart');
    if (!canvas) return;
    const existing = Chart.getChart(canvas);
    if (existing) existing.destroy();
    const actuals = JSON.parse(canvas.dataset.actuals);
    const target  = parseFloat(canvas.dataset.target);
    const labels  = JSON.parse(canvas.dataset.labels);
    const net     = parseFloat(canvas.dataset.net) || 0;
    const goal    = parseFloat(canvas.dataset.savings) || 0;

    // Each period's take-home split into what was spent, what was left for the
    // savings goal, and anything beyond it. A week that spent more than came
    // in shows the overspend on top in red, and the spending bar stops at the
    // take-home so the two add up to what went out. Without a salary there is
    // nothing to split, so the bars are spending alone.
    const spent       = actuals.map(v => net > 0 ? Math.min(v, net) : v);
    const overspent   = actuals.map(v => net > 0 ? Math.max(v - net, 0) : 0);
    // The last bar is the week or month in progress. What it has not spent yet is not
    // saved yet either, so it shows its spending alone.
    const current     = actuals.length - 1;
    const saved       = actuals.map((v, i) => i === current ? 0 : Math.min(Math.max(net - v, 0), goal));
    const unallocated = actuals.map((v, i) => i === current ? 0 : Math.max(net - v - saved[i], 0));
    if (current >= 0) labels[current] += ' (so far)';

    const money = v => '$' + v.toLocaleString(undefined, {minimumFractionDigits: 2, maximumFractionDigits: 2});
    const bar = (label, data, colour) => ({
        type: 'bar', label, data, stack: 'week',
        backgroundColor: colour, borderWidth: 0, order: 2,
    });
    const datasets = [bar('Spending', spent, 'rgba(13,110,253,0.7)')];
    if (net > 0) {
        datasets.push(
            bar('Overspent', overspent, 'rgba(220,53,69,0.8)'),
            bar('Savings', saved, 'rgba(25,135,84,0.75)'),
            bar('Unallocated', unallocated, 'rgba(108,117,125,0.35)'),
        );
    }
    // Take-home is the line spending has to stay under: dotted green, to
    // tell it from the budget target's grey dashes.
    if (net > 0) {
        datasets.push({
            type: 'line',
            label: 'Take-home Pay',
            // Lines get a stack each: the y axis stacks the bars, and a line
            // left in the default stack is drawn on top of the one before it.
            stack: 'take-home',
            data: Array(actuals.length).fill(net),
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
