export class MetricCard extends HTMLElement {
  connectedCallback() {
    this.render();
  }

  private render() {
    const value = this.getAttribute('value') || '—';
    const label = this.getAttribute('label') || 'Metric';
    const unit = this.getAttribute('unit') || '';
    const size = this.getAttribute('size') || 'medium'; // small, medium, large

    const sizeClass = `metric--${size}`;

    this.innerHTML = `
      <div class="metric ${sizeClass}">
        <div class="metric__value">${value}</div>
        <div class="metric__unit">${unit}</div>
        <div class="metric__label">${label}</div>
      </div>
    `;

    this.applyStyles();
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

      .metric {
        background: var(--color-bg-light);
        padding: 12px 16px;
        border-radius: 4px;
        text-align: center;
        font-family: 'Georgia', serif;
      }

      .metric--small {
        padding: 8px 12px;
      }

      .metric--medium {
        padding: 12px 16px;
      }

      .metric--large {
        padding: 16px 20px;
      }

      .metric__value {
        font-size: 24px;
        font-weight: 700;
        color: var(--color-primary);
        line-height: 1.2;
      }

      .metric--large .metric__value {
        font-size: 36px;
      }

      .metric--small .metric__value {
        font-size: 18px;
      }

      .metric__unit {
        font-size: 12px;
        color: var(--color-text-muted);
        font-family: 'Helvetica', sans-serif;
        margin-top: 2px;
      }

      .metric__label {
        font-size: 11px;
        color: var(--color-text-muted);
        text-transform: uppercase;
        letter-spacing: 0.5px;
        margin-top: 4px;
        font-family: 'Helvetica', sans-serif;
      }
    `;
    this.appendChild(style);
  }
}

customElements.define('metric-card', MetricCard);
