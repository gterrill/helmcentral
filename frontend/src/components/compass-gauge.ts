export class CompassGauge extends HTMLElement {
  connectedCallback() {
    this.render();
  }

  private render() {
    const heading = parseFloat(this.getAttribute('heading') || '0');
    const wind = parseFloat(this.getAttribute('wind') || '0');
    const speed = this.getAttribute('speed') || '0';
    const speedUnit = this.getAttribute('unit') || 'kts';

    this.innerHTML = `
      <div class="compass-wrapper">
        <svg class="compass" viewBox="0 0 200 200" width="300" height="300">
          <!-- Background circle -->
          <circle cx="100" cy="100" r="95" fill="#E8DCC8" stroke="#D4A574" stroke-width="1"/>
          
          <!-- Cardinal directions -->
          <text x="100" y="25" text-anchor="middle" class="compass__cardinal">N</text>
          <text x="175" y="105" text-anchor="middle" class="compass__cardinal">E</text>
          <text x="100" y="185" text-anchor="middle" class="compass__cardinal">S</text>
          <text x="25" y="105" text-anchor="middle" class="compass__cardinal">W</text>

          <!-- Intercardinal directions -->
          <text x="150" y="50" text-anchor="middle" class="compass__intercardinal">NE</text>
          <text x="150" y="150" text-anchor="middle" class="compass__intercardinal">SE</text>
          <text x="50" y="150" text-anchor="middle" class="compass__intercardinal">SW</text>
          <text x="50" y="50" text-anchor="middle" class="compass__intercardinal">NW</text>

          <!-- Degree markers -->
          <g class="compass__markers">
            ${this.generateDegreeMarkers()}
          </g>

          <!-- Wind direction arrow (red) -->
          <g transform="rotate(${wind} 100 100)">
            <path d="M 100 20 L 95 35 L 100 30 L 105 35 Z" fill="#C41E3A" stroke="none"/>
            <line x1="100" y1="35" x2="100" y2="100" stroke="#C41E3A" stroke-width="2"/>
          </g>

          <!-- Heading pointer (dark) -->
          <g transform="rotate(${heading} 100 100)">
            <path d="M 100 15 L 95 32 L 100 25 L 105 32 Z" fill="#1B6B6B" stroke="none"/>
            <line x1="100" y1="32" x2="100" y2="95" stroke="#1B6B6B" stroke-width="2"/>
          </g>

          <!-- Center circle -->
          <circle cx="100" cy="100" r="8" fill="#1B6B6B"/>
        </svg>

        <div class="compass__info">
          <div class="compass__speed">${speed}</div>
          <div class="compass__unit">${speedUnit}</div>
        </div>
      </div>
    `;

    this.applyStyles();
  }

  private generateDegreeMarkers() {
    let markers = '';
    for (let i = 0; i < 360; i += 10) {
      const isMajor = i % 30 === 0;
      const yStart = 100 - 90;
      const yEnd = 100 - 85 + (isMajor ? 3 : 0);
      
      markers += `
        <g transform="rotate(${i} 100 100)">
          <line x1="100" y1="${yStart}" x2="100" y2="${yEnd}" 
                stroke="${isMajor ? '#D4A574' : '#CCC'}" 
                stroke-width="${isMajor ? '1.5' : '1'}"/>
        </g>
      `;
    }
    return markers;
  }

  private applyStyles() {
    const style = document.createElement('style');
    style.textContent = `
      :host {
        --color-primary: #1B6B6B;
        --color-accent: #D4A574;
        --color-bg: #E8DCC8;
      }

      .compass-wrapper {
        display: flex;
        flex-direction: column;
        align-items: center;
        gap: 16px;
      }

      .compass {
        max-width: 300px;
        filter: drop-shadow(0 2px 4px rgba(0,0,0,0.1));
      }

      .compass__cardinal {
        font-size: 18px;
        font-weight: bold;
        fill: var(--color-primary);
        font-family: 'Helvetica', sans-serif;
      }

      .compass__intercardinal {
        font-size: 12px;
        fill: #666;
        font-family: 'Helvetica', sans-serif;
      }

      .compass__info {
        text-align: center;
      }

      .compass__speed {
        font-size: 32px;
        font-weight: bold;
        color: var(--color-primary);
        font-family: 'Georgia', serif;
      }

      .compass__unit {
        font-size: 12px;
        color: #888;
        text-transform: uppercase;
        letter-spacing: 0.5px;
      }
    `;
    this.appendChild(style);
  }
}

customElements.define('compass-gauge', CompassGauge);
