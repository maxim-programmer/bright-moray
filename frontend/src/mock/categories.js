// Дерево категорий: категория → подкатегории.
// id используются в адресной строке (/lots?cat=computers&sub=laptops), поэтому только латиница.
export const categories = [
  {
    id: 'furniture',
    name: 'Мебель',
    description: 'Столы, стулья, шкафы и всё, что поможет обустроить комнату',
    subs: [
      { id: 'tables', name: 'Столы' },
      { id: 'chairs', name: 'Стулья и кресла' },
      { id: 'wardrobes', name: 'Шкафы и комоды' },
      { id: 'shelves', name: 'Полки и стеллажи' },
      { id: 'beds', name: 'Кровати и матрасы' }
    ]
  },
  {
    id: 'computers',
    name: 'Компьютеры',
    description: 'Ноутбуки, мониторы, комплектующие и периферия',
    subs: [
      { id: 'laptops', name: 'Ноутбуки' },
      { id: 'desktops', name: 'Системные блоки' },
      { id: 'monitors', name: 'Мониторы' },
      { id: 'parts', name: 'Комплектующие' },
      { id: 'peripherals', name: 'Клавиатуры и мыши' },
      { id: 'drives', name: 'Накопители' }
    ]
  },
  {
    id: 'gadgets',
    name: 'Телефоны и гаджеты',
    description: 'Смартфоны, планшеты, часы и аксессуары к ним',
    subs: [
      { id: 'smartphones', name: 'Смартфоны' },
      { id: 'tablets', name: 'Планшеты и электронные книги' },
      { id: 'watches', name: 'Умные часы и браслеты' },
      { id: 'chargers', name: 'Зарядки и кабели' },
      { id: 'cases', name: 'Чехлы и аксессуары' }
    ]
  },
  {
    id: 'audio',
    name: 'Аудио, ТВ и игры',
    description: 'Наушники, колонки, телевизоры, приставки и камеры',
    subs: [
      { id: 'headphones', name: 'Наушники' },
      { id: 'speakers', name: 'Колонки' },
      { id: 'tv', name: 'Телевизоры и проекторы' },
      { id: 'consoles', name: 'Игровые приставки' },
      { id: 'cameras', name: 'Фото и видеокамеры' }
    ]
  },
  {
    id: 'appliances',
    name: 'Бытовая техника',
    description: 'Техника для общей кухни, уборки и комфорта в комнате',
    subs: [
      { id: 'kettles', name: 'Чайники и кофеварки' },
      { id: 'cooking', name: 'Микроволновки и плитки' },
      { id: 'kitchen-small', name: 'Мелкая кухонная техника' },
      { id: 'cleaning', name: 'Пылесосы и уборка' },
      { id: 'care', name: 'Утюги и фены' },
      { id: 'climate', name: 'Обогреватели и вентиляторы' }
    ]
  },
  {
    id: 'home',
    name: 'Дом и быт',
    description: 'Посуда, текстиль, свет и мелочи для уюта',
    subs: [
      { id: 'dishes', name: 'Посуда' },
      { id: 'textile', name: 'Постельное и пледы' },
      { id: 'lighting', name: 'Освещение' },
      { id: 'decor', name: 'Декор и растения' },
      { id: 'bath', name: 'Ванная' },
      { id: 'organizers', name: 'Хранение и органайзеры' }
    ]
  },
  {
    id: 'study',
    name: 'Учёба',
    description: 'Учебники, конспекты, канцелярия и книги',
    subs: [
      { id: 'textbooks', name: 'Учебники' },
      { id: 'notes', name: 'Конспекты и шпаргалки' },
      { id: 'stationery', name: 'Канцелярия' },
      { id: 'calculators', name: 'Калькуляторы' },
      { id: 'fiction', name: 'Художественная литература' }
    ]
  },
  {
    id: 'hobby',
    name: 'Хобби и творчество',
    description: 'Музыка, настолки, рисование и видеоигры',
    subs: [
      { id: 'music', name: 'Музыкальные инструменты' },
      { id: 'boardgames', name: 'Настольные игры' },
      { id: 'art', name: 'Рисование и рукоделие' },
      { id: 'puzzles', name: 'Пазлы и конструкторы' },
      { id: 'videogames', name: 'Видеоигры' }
    ]
  },
  {
    id: 'sport',
    name: 'Спорт и туризм',
    description: 'Инвентарь для тренировок и походов',
    subs: [
      { id: 'fitness', name: 'Фитнес и тренажёры' },
      { id: 'camping', name: 'Туризм и походы' },
      { id: 'winter', name: 'Зимний спорт' },
      { id: 'ball', name: 'Игры с мячом' },
      { id: 'yoga', name: 'Йога и растяжка' }
    ]
  },
  {
    id: 'collect',
    name: 'Коллекционирование',
    description: 'Монеты, марки, винтаж, фигурки и пластинки',
    subs: [
      { id: 'coins', name: 'Монеты' },
      { id: 'stamps', name: 'Марки и открытки' },
      { id: 'vintage', name: 'Винтаж и антиквариат' },
      { id: 'figures', name: 'Фигурки' },
      { id: 'vinyl', name: 'Пластинки и кассеты' }
    ]
  },
  {
    id: 'clothes',
    name: 'Одежда и обувь',
    description: 'Одежда на каждый день, спорт и аксессуары',
    subs: [
      { id: 'outerwear', name: 'Верхняя одежда' },
      { id: 'everyday', name: 'Повседневная одежда' },
      { id: 'sportswear', name: 'Спортивная одежда' },
      { id: 'shoes', name: 'Обувь' },
      { id: 'bags', name: 'Сумки и рюкзаки' },
      { id: 'accessories', name: 'Аксессуары' }
    ]
  },
  {
    id: 'transport',
    name: 'Транспорт',
    description: 'Велосипеды, самокаты и всё для них',
    subs: [
      { id: 'bikes', name: 'Велосипеды' },
      { id: 'scooters', name: 'Самокаты' },
      { id: 'electric', name: 'Электротранспорт' },
      { id: 'bike-parts', name: 'Запчасти и аксессуары' }
    ]
  }
]

export const dorms = [
  { id: 1, name: 'ЦСО-1' },
  { id: 2, name: 'ЦСО-3' },
  { id: 3, name: 'ЦСО-4' }
]
