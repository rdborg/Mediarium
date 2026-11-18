package books

import "testing"

// TestMatchesCorpus is a table of release names (ebook, audiobook, comic) held
// against books whose titles are easy to confuse: short titles inside longer
// ones, sequels, series numbers, accents, initials and non-Latin names.
func TestMatchesCorpus(t *testing.T) {
	type c struct {
		release string
		want    bool
	}
	groups := []struct {
		book  Book
		cases []c
	}{
		{Book{Title: "Dune", Author: "Frank Herbert"}, []c{
			{"Frank Herbert - Dune (1965) [EPUB]", true},
			{"Frank.Herbert-Dune.Retail.EPUB.eBook-GRP", true},
			{"Frank_Herbert-Dune-AUDIOBOOK-WEB-EN-2007-GRP", true},
			{"Dune by Frank Herbert unabridged m4b", true},
			{"Herbert, Frank - Dune.epub", true},
			{"Frank Herbert - Dune Messiah (1969) [EPUB]", false},
			{"Frank Herbert - Children of Dune epub", false},
			{"Frank Herbert - God Emperor of Dune epub", false},
			{"Frank Herbert - Dune 02 - Dune Messiah epub", false},
			{"Frank Herbert - Dune Chronicles 01 - Dune epub", true},
			{"Brian Herbert - Dune The Duke of Caladan epub", false},
			{"Frank Herbert - Dune (Dune Chronicles #1) epub", true},
			{"Frank Herbert - The Dune Encyclopedia epub", false},
			{"Dune - Frank Herbert.mobi", true},
			{"Dune.epub", false},
			{"Frank Herbert - Dune - 50th Anniversary Edition epub", true},
		}},
		{Book{Title: "Harry Potter and the Philosopher's Stone", Author: "J.K. Rowling"}, []c{
			{"J.K. Rowling - Harry Potter and the Philosopher's Stone epub", true},
			{"J.K. Rowling - Harry Potter and the Philosophers Stone epub", true},
			{"J.K. Rowling - Harry Potter 1 - Harry Potter and the Philosopher's Stone epub", true},
			{"J.K. Rowling - Harry Potter and the Sorcerer's Stone epub", false},
			{"Rowling - Harry Potter and the Chamber of Secrets epub", false},
			{"J.K.Rowling-Harry.Potter.And.The.Philosophers.Stone.Retail.EPUB-GRP", true},
			{"Stephen Fry - Harry Potter and the Philosopher's Stone unabridged mp3", false},
		}},
		{Book{Title: "1984", Author: "George Orwell"}, []c{
			{"George Orwell - 1984 (1949) [EPUB]", true},
			{"George.Orwell-1984.Retail.EPUB-GRP", true},
			{"George Orwell - Nineteen Eighty-Four epub", false},
			{"George Orwell - Animal Farm 1984 epub", false},
			{"George Orwell - 1984 and Animal Farm epub", false},
		}},
		{Book{Title: "Foundation", Author: "Isaac Asimov"}, []c{
			{"Isaac Asimov - Foundation epub", true},
			{"Isaac Asimov - Foundation (Foundation #1) epub", true},
			{"Isaac Asimov - Foundation and Empire epub", false},
			{"Isaac Asimov - Second Foundation epub", false},
			{"Isaac Asimov - Foundation's Edge epub", false},
			{"Isaac Asimov - Prelude to Foundation epub", false},
			{"Asimov - Foundation 1951 epub", true},
		}},
		{Book{Title: "Emma", Author: "Jane Austen"}, []c{
			{"Jane Austen - Emma epub", true},
			{"Jane Austen - Emma (1815) [EPUB]", true},
			{"Jane Austen - Pride and Prejudice epub", false},
			{"Jane Austen - Northanger Abbey and Emma epub", false},
			{"Emma Donoghue - Room epub", false},
		}},
		{Book{Title: "Room", Author: "Emma Donoghue"}, []c{
			{"Emma Donoghue - Room epub", true},
			{"Emma Donoghue - Room (2010) EPUB", true},
			{"Emma Donoghue - The Pull of the Stars epub", false},
			{"Jane Austen - Emma epub", false},
		}},
		{Book{Title: "Le Père Goriot", Author: "Honoré de Balzac"}, []c{
			{"Honoré de Balzac - Le Père Goriot epub", true},
			{"Honore de Balzac - Le Pere Goriot epub", true},
			{"Balzac - Pere Goriot epub", true},
			{"Balzac - Eugénie Grandet epub", false},
		}},
		{Book{Title: "L'Étranger", Author: "Albert Camus"}, []c{
			{"Albert Camus - L'Étranger epub", true},
			{"Albert Camus - L’Étranger epub", true},
			{"Albert Camus - LEtranger epub", true},
			{"Albert Camus - L Etranger epub", false},
			{"Albert Camus - La Peste epub", false},
		}},
		{Book{Title: "Die Verwandlung", Author: "Franz Kafka"}, []c{
			{"Franz Kafka - Die Verwandlung epub", true},
			{"Kafka - Verwandlung.epub", true},
			{"Franz Kafka - Der Prozess epub", false},
			{"Franz Kafka - Die Verwandlung und andere Erzählungen epub", false},
		}},
		{Book{Title: "Преступление и наказание", Author: "Фёдор Достоевский"}, []c{
			{"Фёдор Достоевский - Преступление и наказание epub", true},
			{"Достоевский - Преступление и наказание.fb2", true},
			{"Достоевский - Идиот epub", false},
			{"Лев Толстой - Преступление и наказание epub", false},
		}},
		{Book{Title: "吾輩は猫である", Author: "夏目漱石"}, []c{
			{"夏目漱石 - 吾輩は猫である epub", true},
			{"夏目漱石 - 坊っちゃん epub", false},
		}},
		{Book{Title: "The Lord of the Rings", Author: "J. R. R. Tolkien"}, []c{
			{"J.R.R. Tolkien - The Lord of the Rings epub", true},
			{"Tolkien - Lord of the Rings epub", true},
			{"Tolkien - The Lord of the Rings 1 - The Fellowship of the Ring epub", false},
			{"Tolkien - The Fellowship of the Ring epub", false},
			{"Tolkien - The Lord of the Rings Trilogy epub", false},
			{"Tolkien - The Lord of the Rings (Illustrated) epub", true},
			{"Tolkien - The Lord of the Rings unabridged m4b", true},
		}},
		{Book{Title: "It", Author: "Stephen King"}, []c{
			{"Stephen King - It epub", true},
			{"Stephen King - It (Special Edition) epub", true},
			{"Stephen King - Misery epub", false},
			{"Stephen King - It Ends With Us epub", false},
			{"Colleen Hoover - It Ends With Us epub", false},
			{"Stephen King - It 1986 EPUB", true},
			{"Stephen King - Its epub", false},
		}},
		{Book{Title: "Atomic Habits", Author: "James Clear"}, []c{
			{"James Clear - Atomic Habits epub", true},
			{"James Clear - Atomic Habits - An Easy & Proven Way to Build Good Habits epub", false},
			{"James Clear - Atomic Habits (2018) [EPUB]", true},
			{"Atomic.Habits.James.Clear.Audiobook.M4B", true},
			{"James Clear - Atomic Habits Workbook pdf", false},
			{"Clear - Atomic Habits.mobi", true},
		}},
		{Book{Title: "Who Moved My Cheese?", Author: "Spencer Johnson"}, []c{
			{"Spencer Johnson - Who Moved My Cheese epub", true},
			{"Spencer Johnson - Who Moved My Cheese? epub", true},
		}},
		{Book{Title: "Ender's Game", Author: "Orson Scott Card"}, []c{
			{"Orson Scott Card - Ender's Game epub", true},
			{"Orson Scott Card - Enders Game epub", true},
			{"Orson Scott Card - Ender Game epub", false},
			{"Orson Scott Card - Ender's Shadow epub", false},
			{"Card - Enders.Game.Retail.EPUB-GRP", true},
		}},
		{Book{Title: "Good Omens", Author: "Terry Pratchett"}, []c{
			{"Terry Pratchett - Good Omens epub", true},
			{"Neil Gaiman - Good Omens epub", false},
		}},
		{Book{Title: "Watchmen", Author: "Alan Moore"}, []c{
			{"Alan Moore - Watchmen (1986-1987) (digital) cbz", true},
			{"Watchmen (1986) (Alan Moore) cbr", true},
			{"Watchmen (1986) cbr", false},
			{"Alan Moore - Before Watchmen cbz", false},
		}},
	}
	checked := 0
	for _, g := range groups {
		for _, tc := range g.cases {
			checked++
			if got := Matches(tc.release, g.book); got != tc.want {
				t.Errorf("%q for %q by %s: %v, want %v", tc.release, g.book.Title, g.book.Author, got, tc.want)
			}
		}
	}
	t.Logf("%d book releases checked", checked)
}
