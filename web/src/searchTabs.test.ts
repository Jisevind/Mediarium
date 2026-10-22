import assert from 'node:assert/strict'
import { test } from 'node:test'
import type { BookFound } from './api.ts'
import { booksTabLabel, resultsLink, resultsParams, resultsTab, splitBooks } from './searchTabs.ts'

test('the tab in the address wins when it can be shown', () => {
  const cases: [string | null, boolean, boolean, string][] = [
    [null, true, true, 'titles'],
    ['books', true, true, 'books'],
    ['titles', true, true, 'titles'],
    ['books', true, false, 'titles'], // books are switched off
    ['titles', false, true, 'books'], // movies and shows are switched off
    [null, false, true, 'books'],
    [null, false, false, 'titles'],
    ['nonsense', true, true, 'titles'],
  ]
  for (const [param, titlesOn, booksOn, want] of cases) assert.equal(resultsTab(param, titlesOn, booksOn), want, `${param} ${titlesOn} ${booksOn}`)
})

test('links keep the term and name the tab only for books', () => {
  assert.equal(resultsLink('titles', 'Dune'), '/search?q=Dune')
  assert.equal(resultsLink('books', 'Dune'), '/search?q=Dune&tab=books')
  assert.equal(resultsLink('books', ''), '/search?tab=books')
  assert.equal(resultsLink('titles', '  '), '/search')
  assert.equal(resultsLink('books', 'Tolkien & Co'), '/search?q=Tolkien+%26+Co&tab=books')
  assert.deepEqual(resultsParams('books', ' Dune '), { q: 'Dune', tab: 'books' })
  assert.deepEqual(resultsParams('titles', 'Dune'), { q: 'Dune' })
  assert.deepEqual(resultsParams('titles', ''), {})
})

test('the Books tab shows how many books were found', () => {
  assert.equal(booksTabLabel(null), 'Books')
  assert.equal(booksTabLabel(0), 'Books (0)')
  assert.equal(booksTabLabel(12), 'Books (12)')
})

test('books in the library come first, and a book added on the page moves up', () => {
  const list: BookFound[] = [
    { key: 'OL1W', title: 'A', author: 'X' },
    { key: 'OL2W', title: 'B', author: 'X', libraryId: 7 },
    { key: 'OL3W', title: 'C', author: 'Y' },
  ]
  const plain = splitBooks(list)
  assert.deepEqual(plain.owned.map((b) => b.key), ['OL2W'])
  assert.deepEqual(plain.fresh.map((b) => b.key), ['OL1W', 'OL3W'])
  const after = splitBooks(list, { OL3W: 9 })
  assert.deepEqual(after.owned.map((b) => `${b.key}:${b.libraryId}`), ['OL2W:7', 'OL3W:9'])
  assert.deepEqual(after.fresh.map((b) => b.key), ['OL1W'])
  assert.deepEqual(splitBooks([]), { owned: [], fresh: [] })
})
