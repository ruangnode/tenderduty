// Block status → color  (StatusType enum from ws.go)
// 0=missed  1=prevote  2=precommit  3=signed  4=proposer  -1/other=nodata
const TL_COLOR = {
    4: '#3b82f6',
    3: '#10b981',
    2: '#f97316',
    1: '#a855f7',
    0: '#ef4444',
}

// Build mini-bar HTML for a blocks array.
// slots: max number of bars to show (calculated by caller from available width)
function buildBlockBars(blocks, slots) {
    if (!blocks || blocks.length === 0) {
        console.log('[RN] buildBlockBars: empty blocks array')
        return '<span class="rn-hist-wait">Menunggu data dari Tenderduty…</span>'
    }

    const n    = slots || 150
    const show = blocks.length > n ? blocks.slice(blocks.length - n) : blocks
    const allNoData = show.every(b => b === -1 || b === null || b === undefined)

    if (allNoData) {
        // Still render bars so user sees the slots, but warn in console
        console.log('[RN] buildBlockBars: all blocks are no-data (-1), chain may just have started')
    }

    let html = ''
    for (const b of show) {
        const c = TL_COLOR[b]
        html += c
            ? `<span class="tl-bar" style="--bc:${c}"></span>`
            : `<span class="tl-bar-nd"></span>`
    }
    return html
}

// drawSeries is now a no-op — block bars are rendered inline per chain in updateTable()
function drawSeries() {}

// legend() kept for onload compatibility
function legend() {}

// ── Theme ─────────────────────────────────────────────────────────────────
function _syncThemeBtn() {
    const t   = document.documentElement.getAttribute('data-theme') || 'dark'
    const btn = document.getElementById('theme-btn')
    if (btn) btn.textContent = t === 'dark' ? '☀ Light' : '🌙 Dark'
}

function toggleTheme() {
    const t    = document.documentElement.getAttribute('data-theme') || 'dark'
    const next = t === 'dark' ? 'light' : 'dark'
    document.documentElement.setAttribute('data-theme', next)
    localStorage.setItem('rn-theme', next)
    _syncThemeBtn()
}

function lightMode() { toggleTheme() }

document.addEventListener('DOMContentLoaded', _syncThemeBtn)
