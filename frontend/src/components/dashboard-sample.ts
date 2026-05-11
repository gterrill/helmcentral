import './metric-card';
import './compass-gauge';

export class DashboardSample extends HTMLElement {
  connectedCallback() {
    this.render();
  }

  private render() {
    this.innerHTML = `
      <div class="dashboard-sample">
        <header class="dashboard-header">
          <div class="header-left">
            <h1 class="title">S/V INGENUITY</h1>
            <span class="location">2024-05 SIGNALK ST. ANDREWS</span>
          </div>
          <div class="header-right">
            <div class="status-indicator">
              <span class="status-dot"></span>
              <span class="status-text">DAY MODE</span>
            </div>
            <time class="current-time">10:57:11 AM</time>
          </div>
        </header>

        <main class="dashboard-grid">
          <!-- Left Panel -->
          <aside class="panel panel--left">
            <section class="section">
              <h3 class="section__title">DEPTH</h3>
              <div class="metric-large">
                <div class="metric-large__value">6.3</div>
                <div class="metric-large__unit">ft</div>
                <div class="metric-large__label">122 ft total depth</div>
              </div>
            </section>

            <section class="section">
              <h3 class="section__title">POSITION</h3>
              <div class="position-box">
                <div class="position-row">
                  <span class="position-label">LAT</span>
                  <span class="position-value">25°29'10.2"N</span>
                </div>
                <div class="position-row">
                  <span class="position-label">LON</span>
                  <span class="position-value">76°28'14.0"W</span>
                </div>
              </div>
              <div class="heading-box">
                <span class="heading-value">63°</span>
                <span class="heading-unit">ENE</span>
              </div>
            </section>

            <section class="section">
              <h3 class="section__title">NEARBY VESSELS</h3>
              <div class="vessel-list">
                <div class="vessel-item">
                  <span class="vessel-name">LONG SHADOW</span>
                  <span class="vessel-distance">350 ft</span>
                </div>
                <div class="vessel-item">
                  <span class="vessel-name">SOUTHLAND LOVE</span>
                  <span class="vessel-distance">538 ft</span>
                </div>
                <div class="vessel-item">
                  <span class="vessel-name">SALT SHAKER</span>
                  <span class="vessel-distance">1359 ft</span>
                </div>
                <div class="vessel-item">
                  <span class="vessel-name">ZUMA</span>
                  <span class="vessel-distance">1425 ft</span>
                </div>
              </div>
            </section>

            <section class="section">
              <h3 class="section__title">TODAY & NOW</h3>
              <div class="weather-box">
                <div class="weather-icon">☀️</div>
                <div class="weather-temp">75°F</div>
                <div class="weather-condition">MOSTLY CLEAR</div>
                <div class="weather-wind">↑ 8 pmph</div>
              </div>
              <div class="tide-info">
                <div class="tide-level">1.4 m</div>
                <div class="tide-label">TIDE NOW</div>
                <div class="tide-changes">
                  <div class="tide-change">⬆ Today 12:57 PM (+1.3)</div>
                  <div class="tide-change">⬇ Today 7:39 PM (-1.0)</div>
                </div>
              </div>
            </section>
          </aside>

          <!-- Center Panel - Main Compass -->
          <section class="panel panel--center">
            <div class="section">
              <h3 class="section__title">APPARENT WIND - COURSE UP</h3>
              <compass-gauge heading="135" wind="30" speed="13" unit="kts"></compass-gauge>
              <div class="wind-info">
                <div class="wind-detail">
                  <span class="wind-label">AWA</span>
                  <span class="wind-value">061°</span>
                </div>
              </div>
              <div class="gust-info">
                <span class="gust-label">MAX GUST 10 NM</span>
                <span class="gust-value">21 NM</span>
              </div>
            </section>
          </section>

          <!-- Right Panel -->
          <aside class="panel panel--right">
            <section class="section">
              <h3 class="section__title">⚓ ANCHOR WATCH</h3>
              <div class="anchor-metrics">
                <metric-card value="123" unit="ft" label="Anchor Distance" size="medium"></metric-card>
                <metric-card value="66" unit="°" label="Bearing" size="medium"></metric-card>
                <metric-card value="160" unit="ft" label="Chain Out" size="medium"></metric-card>
              </div>
              <div class="anchor-position">
                <div class="anchor-visual">
                  <svg viewBox="0 0 120 120" width="100" height="100">
                    <circle cx="60" cy="60" r="55" fill="#E0F2F7" stroke="#1B6B6B" stroke-width="1"/>
                    <circle cx="60" cy="60" r="40" fill="none" stroke="#A0D8E8" stroke-width="1" stroke-dasharray="2,2"/>
                    <circle cx="60" cy="10" r="6" fill="#1B6B6B"/>
                    <text x="60" y="65" text-anchor="middle" font-size="10" fill="#666">123 ft</text>
                  </svg>
                </div>
                <div class="anchor-coords">
                  <div>LAT: 25 28.181 'N</div>
                  <div>LON: 76 38.213 'W</div>
                </div>
              </div>
              <div class="anchor-radius">
                <label>ALARM RADIUS (FT)</label>
                <input type="number" value="160" class="anchor-input">
                <button class="btn btn--accent">SET ANCHOR</button>
                <button class="btn btn--secondary">CLEAR</button>
              </div>
            </section>

            <section class="section">
              <h3 class="section__title">BATTERY & POWER</h3>
              <div class="battery-box">
                <div class="battery-percentage">
                  <div class="battery-number">68<span class="battery-percent">%</span></div>
                  <div class="battery-label">CHARGING</div>
                  <div class="battery-value">+24.8 A</div>
                </div>
              </div>
              <div class="power-metrics">
                <div class="power-row">
                  <span class="power-label">SOLAR OUTPUT</span>
                  <span class="power-value">1868 W</span>
                </div>
                <div class="power-row">
                  <span class="power-label">AC OUTPUT</span>
                  <span class="power-value">1017 W</span>
                </div>
                <div class="power-row">
                  <span class="power-label">12V DC POWER</span>
                  <span class="power-value">125 A 4.7A</span>
                </div>
                <div class="power-row">
                  <span class="power-label">24V VOLTAGE</span>
                  <span class="power-value">26.73 V</span>
                </div>
              </div>
              <div class="load-info">
                <span class="load-label">EMPORIA A2 LOADS</span>
                <span class="load-value">849 W</span>
                <span class="load-detail">Dishwasher/Gallery 600W+outlet Sink/Cooktop 112W</span>
              </div>
            </section>

            <section class="section">
              <h3 class="section__title">TANKS</h3>
              <div class="tank-list">
                <div class="tank-row">
                  <span class="tank-label">FRESH WATER</span>
                  <div class="tank-bar">
                    <div class="tank-fill" style="width: 63%"></div>
                  </div>
                  <span class="tank-percent">63%</span>
                </div>
                <div class="tank-row">
                  <span class="tank-label">STG AFT POD</span>
                  <div class="tank-bar">
                    <div class="tank-fill" style="width: 42%"></div>
                  </div>
                  <span class="tank-percent">42%</span>
                </div>
                <div class="tank-row">
                  <span class="tank-label">PORT FUEL</span>
                  <div class="tank-bar">
                    <div class="tank-fill" style="width: 50%; background: #D4A574;"></div>
                  </div>
                  <span class="tank-percent">50%</span>
                </div>
              </div>
            </section>
          </aside>
        </main>

        <footer class="dashboard-footer">
          <div class="footer-tabs">
            <button class="footer-tab active">FORECAST GRAPH</button>
            <button class="footer-tab">FORECAST TABLE</button>
            <button class="footer-tab">TIDES</button>
            <button class="footer-tab">RADAR</button>
            <button class="footer-tab">SKY</button>
            <button class="footer-tab">AC LOADS</button>
            <button class="footer-tab">AC HISTORY</button>
            <button class="footer-tab">SOLAR</button>
          </div>
        </footer>
      </div>
    `;

    this.applyStyles();
    this.updateTime();
    setInterval(() => this.updateTime(), 1000);
  }

  private updateTime() {
    const timeEl = this.querySelector('.current-time');
    if (timeEl) {
      const now = new Date();
      const hours = String(now.getHours()).padStart(2, '0');
      const minutes = String(now.getMinutes()).padStart(2, '0');
      const seconds = String(now.getSeconds()).padStart(2, '0');
      const period = now.getHours() >= 12 ? 'PM' : 'AM';
      timeEl.textContent = `${hours}:${minutes}:${seconds} ${period}`;
    }
  }

  private applyStyles() {
    const style = document.createElement('style');
    style.textContent = `
      :host {
        --color-primary: #1B6B6B;
        --color-accent: #D4A574;
        --color-bg: #E8DCC8;
        --color-bg-light: #F4F0EB;
        --color-text: #333;
        --color-text-muted: #888;
      }

      * {
        box-sizing: border-box;
      }

      .dashboard-sample {
        background: var(--color-bg);
        color: var(--color-text);
        font-family: 'Helvetica', sans-serif;
        min-height: 100vh;
        display: flex;
        flex-direction: column;
      }

      .dashboard-header {
        background: var(--color-bg-light);
        padding: 12px 24px;
        display: flex;
        justify-content: space-between;
        align-items: center;
        border-bottom: 1px solid #ddd;
      }

      .header-left {
        display: flex;
        align-items: baseline;
        gap: 12px;
      }

      .title {
        font-size: 18px;
        font-weight: bold;
        color: var(--color-accent);
        margin: 0;
        font-family: 'Georgia', serif;
        letter-spacing: 2px;
      }

      .location {
        font-size: 11px;
        color: var(--color-text-muted);
        text-transform: uppercase;
      }

      .header-right {
        display: flex;
        gap: 24px;
        align-items: center;
      }

      .status-indicator {
        display: flex;
        align-items: center;
        gap: 8px;
        font-size: 12px;
      }

      .status-dot {
        width: 8px;
        height: 8px;
        background: var(--color-primary);
        border-radius: 50%;
      }

      .current-time {
        font-size: 18px;
        font-weight: bold;
        color: var(--color-primary);
        font-family: 'Courier New', monospace;
      }

      .dashboard-grid {
        display: grid;
        grid-template-columns: 200px 1fr 280px;
        gap: 20px;
        padding: 20px;
        flex: 1;
        overflow-y: auto;
      }

      .panel {
        display: flex;
        flex-direction: column;
        gap: 20px;
      }

      .section {
        background: var(--color-bg-light);
        padding: 16px;
        border-radius: 4px;
        border: 1px solid #ddd;
      }

      .section__title {
        font-size: 11px;
        font-weight: bold;
        color: var(--color-text-muted);
        text-transform: uppercase;
        letter-spacing: 1px;
        margin: 0 0 12px 0;
      }

      .metric-large {
        text-align: center;
      }

      .metric-large__value {
        font-size: 44px;
        font-weight: bold;
        color: var(--color-primary);
        font-family: 'Georgia', serif;
        line-height: 1;
      }

      .metric-large__unit {
        font-size: 12px;
        color: var(--color-text-muted);
        margin-top: 4px;
      }

      .metric-large__label {
        font-size: 11px;
        color: var(--color-text-muted);
        margin-top: 8px;
      }

      .position-box,
      .heading-box {
        display: flex;
        flex-direction: column;
        gap: 8px;
        margin-top: 8px;
      }

      .position-row {
        display: flex;
        justify-content: space-between;
        font-size: 12px;
      }

      .position-label {
        color: var(--color-text-muted);
        font-weight: bold;
      }

      .position-value {
        color: var(--color-text);
        font-family: 'Courier New', monospace;
      }

      .heading-box {
        background: #D8E8E8;
        padding: 12px;
        border-radius: 4px;
        text-align: center;
      }

      .heading-value {
        font-size: 28px;
        font-weight: bold;
        color: var(--color-primary);
        font-family: 'Georgia', serif;
      }

      .heading-unit {
        font-size: 12px;
        color: var(--color-text-muted);
      }

      .vessel-list {
        display: flex;
        flex-direction: column;
        gap: 6px;
        font-size: 11px;
      }

      .vessel-item {
        display: flex;
        justify-content: space-between;
        padding: 6px 8px;
        background: white;
        border-radius: 3px;
      }

      .vessel-name {
        font-weight: 500;
        color: var(--color-text);
      }

      .vessel-distance {
        color: var(--color-text-muted);
        font-family: 'Courier New', monospace;
      }

      .weather-box {
        display: grid;
        grid-template-columns: 1fr 1fr;
        gap: 8px;
        text-align: center;
        padding: 12px;
        background: white;
        border-radius: 4px;
      }

      .weather-icon {
        font-size: 32px;
        grid-column: 1 / -1;
      }

      .weather-temp {
        font-size: 24px;
        font-weight: bold;
        color: var(--color-primary);
      }

      .weather-condition {
        font-size: 10px;
        color: var(--color-text-muted);
      }

      .tide-info {
        margin-top: 12px;
        text-align: center;
      }

      .tide-level {
        font-size: 20px;
        font-weight: bold;
        color: var(--color-primary);
      }

      .tide-label {
        font-size: 10px;
        color: var(--color-text-muted);
        text-transform: uppercase;
      }

      .tide-changes {
        font-size: 10px;
        color: var(--color-text-muted);
        margin-top: 6px;
      }

      .tide-change {
        margin: 2px 0;
      }

      .wind-info,
      .gust-info {
        display: flex;
        justify-content: space-between;
        align-items: center;
        padding: 12px;
        background: white;
        border-radius: 4px;
        margin-top: 12px;
      }

      .wind-detail,
      .gust-label {
        font-size: 12px;
      }

      .wind-label {
        color: var(--color-text-muted);
      }

      .wind-value {
        font-size: 24px;
        font-weight: bold;
        color: var(--color-accent);
        font-family: 'Georgia', serif;
        margin-left: 8px;
      }

      .gust-label {
        color: var(--color-text-muted);
      }

      .gust-value {
        font-size: 18px;
        font-weight: bold;
        color: var(--color-accent);
        font-family: 'Georgia', serif;
      }

      .anchor-metrics {
        display: grid;
        grid-template-columns: 1fr;
        gap: 8px;
      }

      .anchor-visual {
        text-align: center;
        margin: 12px 0;
      }

      .anchor-coords {
        font-size: 11px;
        color: var(--color-text-muted);
        text-align: center;
        margin: 8px 0;
      }

      .anchor-radius {
        margin-top: 12px;
      }

      .anchor-radius label {
        font-size: 10px;
        color: var(--color-text-muted);
        text-transform: uppercase;
        display: block;
        margin-bottom: 6px;
      }

      .anchor-input {
        width: 100%;
        padding: 6px;
        border: 1px solid #ccc;
        border-radius: 3px;
        font-size: 12px;
        margin-bottom: 8px;
      }

      .btn {
        padding: 8px 12px;
        border: 1px solid #ccc;
        border-radius: 3px;
        font-size: 11px;
        cursor: pointer;
        text-transform: uppercase;
        font-weight: bold;
        width: 100%;
        margin-bottom: 6px;
      }

      .btn--accent {
        background: var(--color-accent);
        color: white;
        border-color: var(--color-accent);
      }

      .btn--secondary {
        background: white;
        color: var(--color-text);
        border-color: #ccc;
      }

      .battery-box {
        background: white;
        padding: 16px;
        border-radius: 4px;
        text-align: center;
      }

      .battery-number {
        font-size: 36px;
        font-weight: bold;
        color: var(--color-accent);
        font-family: 'Georgia', serif;
      }

      .battery-percent {
        font-size: 24px;
      }

      .battery-label {
        font-size: 11px;
        color: var(--color-text-muted);
        text-transform: uppercase;
      }

      .battery-value {
        font-size: 14px;
        font-weight: bold;
        color: var(--color-primary);
        margin-top: 4px;
      }

      .power-metrics {
        margin-top: 12px;
        display: flex;
        flex-direction: column;
        gap: 8px;
      }

      .power-row {
        display: flex;
        justify-content: space-between;
        font-size: 11px;
        padding: 8px;
        background: white;
        border-radius: 3px;
      }

      .power-label {
        color: var(--color-text-muted);
        font-weight: bold;
      }

      .power-value {
        color: var(--color-text);
        font-weight: bold;
      }

      .load-info {
        margin-top: 12px;
        padding: 12px;
        background: white;
        border-radius: 4px;
        text-align: center;
        font-size: 11px;
      }

      .load-label {
        color: var(--color-text-muted);
        display: block;
        font-weight: bold;
        margin-bottom: 4px;
      }

      .load-value {
        display: block;
        font-size: 18px;
        font-weight: bold;
        color: var(--color-accent);
        font-family: 'Georgia', serif;
        margin-bottom: 4px;
      }

      .load-detail {
        color: var(--color-text-muted);
        font-size: 9px;
      }

      .tank-list {
        display: flex;
        flex-direction: column;
        gap: 12px;
      }

      .tank-row {
        display: grid;
        grid-template-columns: 80px 1fr 40px;
        align-items: center;
        gap: 8px;
        font-size: 11px;
      }

      .tank-label {
        color: var(--color-text-muted);
        font-weight: bold;
      }

      .tank-bar {
        height: 12px;
        background: #E0E0E0;
        border-radius: 2px;
        overflow: hidden;
      }

      .tank-fill {
        height: 100%;
        background: var(--color-primary);
      }

      .tank-percent {
        text-align: right;
        color: var(--color-text-muted);
        font-weight: bold;
      }

      .dashboard-footer {
        background: var(--color-bg-light);
        border-top: 1px solid #ddd;
        padding: 8px 20px;
      }

      .footer-tabs {
        display: flex;
        gap: 12px;
        flex-wrap: wrap;
      }

      .footer-tab {
        padding: 8px 12px;
        background: white;
        border: 1px solid #ccc;
        border-radius: 3px;
        font-size: 10px;
        cursor: pointer;
        text-transform: uppercase;
        font-weight: bold;
        color: var(--color-text-muted);
        transition: all 0.2s;
      }

      .footer-tab:hover {
        background: var(--color-bg);
      }

      .footer-tab.active {
        background: var(--color-accent);
        color: white;
        border-color: var(--color-accent);
      }

      @media (max-width: 1400px) {
        .dashboard-grid {
          grid-template-columns: 1fr;
        }
      }
    `;
    this.appendChild(style);
  }
}

customElements.define('dashboard-sample', DashboardSample);
