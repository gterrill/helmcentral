export class DashboardContainer extends HTMLElement {
  connectedCallback() {
    this.render();
  }

  private render() {
    this.innerHTML = `
      <div class="dashboard">
        <header class="dashboard-header">
          <h1>Helmcentral Dashboard</h1>
        </header>
        <main class="dashboard-content">
          <p>Dashboard loading...</p>
        </main>
      </div>
    `;
    this.applyStyles();
  }

  private applyStyles() {
    const style = document.createElement('style');
    style.textContent = `
      :host {
        --color-primary: #0066cc;
        --color-bg: #ffffff;
        --color-text: #1a1a1a;
        --spacing-unit: 8px;
      }

      .dashboard {
        width: 100%;
        height: 100vh;
        display: flex;
        flex-direction: column;
        background-color: var(--color-bg);
        color: var(--color-text);
      }

      .dashboard-header {
        padding: calc(var(--spacing-unit) * 2);
        border-bottom: 1px solid #e0e0e0;
      }

      .dashboard-header h1 {
        margin: 0;
        font-size: 28px;
      }

      .dashboard-content {
        flex: 1;
        padding: calc(var(--spacing-unit) * 3);
        overflow-y: auto;
      }
    `;
    this.appendChild(style);
  }
}

customElements.define('dashboard-container', DashboardContainer);
