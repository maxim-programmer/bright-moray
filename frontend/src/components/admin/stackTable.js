// Директива v-stack: копирует заголовки столбцов в data-label ячеек.
// На узком экране таблица превращается в карточки (admin.css), и подписи берутся оттуда.
function label(table) {
  const heads = [...table.querySelectorAll('thead th')].map((th) => th.textContent.trim())
  for (const row of table.querySelectorAll('tbody tr')) {
    const cells = [...row.children]
    cells.forEach((cell, i) => cell.setAttribute('data-label', heads[i] || ''))
  }
}

export const vStack = { mounted: label, updated: label }
