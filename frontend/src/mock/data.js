const minute = 60 * 1000
const hour = 60 * minute
const day = 24 * hour
const now = Date.now()

export const users = [
  { id: 1, username: 'anna', password: '123456' },
  { id: 2, username: 'boris', password: '123456' },
  { id: 3, username: 'clara', password: '123456' }
]

export const lots = [
  {
    id: 1,
    title: 'Винтажный фотоаппарат',
    description: 'Плёночный фотоаппарат 1970-х годов. Работает, есть чехол.',
    image: 'https://picsum.photos/seed/camera/600/400',
    authorId: 1,
    startPrice: 1000,
    step: 100,
    endsAt: now + 5 * hour,
    status: 'active'
  },
  {
    id: 2,
    title: 'Настольная лампа',
    description: 'Лампа из латуни в стиле лофт. Состояние отличное.',
    image: 'https://picsum.photos/seed/lamp/600/400',
    authorId: 2,
    startPrice: 500,
    step: 50,
    endsAt: now + 26 * hour,
    status: 'active'
  },
  {
    id: 3,
    title: 'Коллекционная монета',
    description: 'Серебряная монета, 1965 год.',
    image: 'https://picsum.photos/seed/coin/600/400',
    authorId: 1,
    startPrice: 3000,
    step: 250,
    endsAt: now - 3 * hour,
    status: 'finished'
  },
  {
    id: 4,
    title: 'Старая книга',
    description: 'Первое издание, обложка немного потёрта.',
    image: 'https://picsum.photos/seed/book/600/400',
    authorId: 3,
    startPrice: 800,
    step: 100,
    endsAt: now - 10 * hour,
    status: 'finished'
  },
  {
    id: 5,
    title: 'Гитара акустическая',
    description: 'Хорошо звучит, комплект струн новый.',
    image: 'https://picsum.photos/seed/guitar/600/400',
    authorId: 2,
    startPrice: 7000,
    step: 500,
    endsAt: now + 48 * hour,
    status: 'cancelled'
  },
  {
    id: 6,
    title: 'Городской велосипед',
    description: 'Скоростной велосипед, 21 передача. Недавно менял цепь и тормоза.',
    image: 'https://picsum.photos/seed/bicycle/600/400',
    authorId: 3,
    startPrice: 5000,
    step: 300,
    endsAt: now + 2 * hour,
    status: 'active'
  },
  {
    id: 7,
    title: 'Чайный сервиз',
    description: 'Набор на шесть персон: чайник, шесть чашек и блюдца.',
    image: 'https://picsum.photos/seed/teaset/600/400',
    authorId: 1,
    startPrice: 1500,
    step: 100,
    endsAt: now + 12 * hour,
    status: 'active'
  },
  {
    id: 8,
    title: 'Настольная игра «Каркассон»',
    description: 'Полный комплект с двумя дополнениями. Играли пару раз.',
    image: 'https://picsum.photos/seed/boardgame/600/400',
    authorId: 2,
    startPrice: 700,
    step: 50,
    endsAt: now + 3 * day,
    status: 'active'
  },
  {
    id: 9,
    title: 'Туристический рюкзак',
    description: 'Рюкзак на 60 литров, водонепроницаемый. Ни разу не ломался.',
    image: 'https://picsum.photos/seed/backpack/600/400',
    authorId: 3,
    startPrice: 2000,
    step: 200,
    endsAt: now + 8 * hour,
    status: 'active'
  },
  {
    id: 10,
    title: 'Электрочайник',
    description: 'Чайник на 1,7 литра с автоотключением.',
    image: 'https://picsum.photos/seed/kettle/600/400',
    authorId: 2,
    startPrice: 600,
    step: 50,
    endsAt: now + 20 * minute,
    status: 'active'
  },
  {
    id: 11,
    title: 'Конспекты по матанализу',
    description: 'Подробные конспекты за два семестра, написаны от руки.',
    image: 'https://picsum.photos/seed/notes/600/400',
    authorId: 1,
    startPrice: 300,
    step: 50,
    endsAt: now + 4 * day,
    status: 'active'
  },
  {
    id: 12,
    title: 'Монитор 24 дюйма',
    description: 'Full HD, IPS-матрица, кабель HDMI в комплекте.',
    image: 'https://picsum.photos/seed/monitor/600/400',
    authorId: 3,
    startPrice: 6000,
    step: 500,
    endsAt: now + day + 3 * hour,
    status: 'active'
  },
  {
    id: 13,
    title: 'Тёплый плед',
    description: 'Большой шерстяной плед, размер 150 на 200 см.',
    image: 'https://picsum.photos/seed/blanket/600/400',
    authorId: 1,
    startPrice: 900,
    step: 100,
    endsAt: now - 30 * hour,
    status: 'finished'
  },
  {
    id: 14,
    title: 'Микроволновая печь',
    description: 'Мощность 800 Вт, работает без проблем.',
    image: 'https://picsum.photos/seed/microwave/600/400',
    authorId: 2,
    startPrice: 2500,
    step: 200,
    endsAt: now - 2 * day,
    status: 'finished'
  },
  {
    id: 15,
    title: 'Набор посуды',
    description: 'Две кастрюли и сковорода с антипригарным покрытием.',
    image: 'https://picsum.photos/seed/cookware/600/400',
    authorId: 1,
    startPrice: 1800,
    step: 150,
    endsAt: now + 10 * hour,
    status: 'cancelled'
  },
  {
    id: 16,
    title: 'Беспроводные наушники',
    description: 'Накладные наушники, держат заряд около 20 часов.',
    image: 'https://picsum.photos/seed/headphones/600/400',
    authorId: 3,
    startPrice: 2200,
    step: 100,
    endsAt: now + 15 * hour,
    status: 'active'
  }
]

export const bids = [
  { id: 1, lotId: 1, userId: 2, amount: 1100, createdAt: now - 3 * hour },
  { id: 2, lotId: 1, userId: 3, amount: 1250, createdAt: now - 2 * hour },
  { id: 3, lotId: 2, userId: 1, amount: 600, createdAt: now - hour },
  { id: 4, lotId: 3, userId: 2, amount: 3250, createdAt: now - 20 * hour },
  { id: 5, lotId: 3, userId: 3, amount: 3500, createdAt: now - 15 * hour },
  { id: 6, lotId: 6, userId: 1, amount: 5300, createdAt: now - 6 * hour },
  { id: 7, lotId: 6, userId: 2, amount: 5600, createdAt: now - 4 * hour },
  { id: 8, lotId: 6, userId: 1, amount: 6000, createdAt: now - 30 * minute },
  { id: 9, lotId: 8, userId: 3, amount: 750, createdAt: now - 5 * hour },
  { id: 10, lotId: 10, userId: 1, amount: 650, createdAt: now - 2 * hour },
  { id: 11, lotId: 10, userId: 3, amount: 700, createdAt: now - hour },
  { id: 12, lotId: 11, userId: 2, amount: 350, createdAt: now - 8 * hour },
  { id: 13, lotId: 12, userId: 1, amount: 6500, createdAt: now - 10 * hour },
  { id: 14, lotId: 12, userId: 2, amount: 7000, createdAt: now - 3 * hour },
  { id: 15, lotId: 13, userId: 2, amount: 1000, createdAt: now - 40 * hour },
  { id: 16, lotId: 13, userId: 3, amount: 1200, createdAt: now - 35 * hour },
  { id: 17, lotId: 16, userId: 1, amount: 2300, createdAt: now - 2 * hour },
  { id: 18, lotId: 16, userId: 2, amount: 2400, createdAt: now - hour }
]
