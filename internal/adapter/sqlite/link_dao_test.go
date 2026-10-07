package sqlite

import (
	"fmt"
	"slices"
	"testing"

	"github.com/zk-org/zk/internal/core"
	"github.com/zk-org/zk/internal/util"
	"github.com/zk-org/zk/internal/util/test/assert"
)

type linkRow struct {
	SourceID                                     core.NoteID
	TargetID                                     *core.NoteID
	Href, Type, Title, Rels, Snippet             string
	SnippetStart, SnippetEnd, LinkStart, LinkEnd int
	IsExternal                                   bool
}

func queryLinkRows(t *testing.T, q RowQuerier, where string) []linkRow {
	links := make([]linkRow, 0)

	rows, err := q.Query(fmt.Sprintf(`
		SELECT source_id, target_id, title, href, type, external, rels, snippet, snippet_start, snippet_end, link_start, link_end
		  FROM links
		 WHERE %v
		 ORDER BY id
	`, where))
	assert.Nil(t, err)

	for rows.Next() {
		var row linkRow
		var sourceID int64
		var targetID *int64
		err = rows.Scan(&sourceID, &targetID, &row.Title, &row.Href, &row.Type, &row.IsExternal, &row.Rels, &row.Snippet, &row.SnippetStart, &row.SnippetEnd, &row.LinkStart, &row.LinkEnd)
		assert.Nil(t, err)
		row.SourceID = core.NoteID(sourceID)
		if targetID != nil {
			row.TargetID = idPointer(*targetID)
		}
		links = append(links, row)
	}
	rows.Close()
	assert.Nil(t, rows.Err())

	return links
}

func TestLinkDAOFindTouchingNotes(t *testing.T) {
	testLinkDAO(t, func(tx Transaction, dao *LinkDAO) {
		links, err := dao.FindTouchingNotes([]core.NoteID{1, 4})
		assert.Nil(t, err)
		var ids []int
		for _, link := range links {
			ids = append(ids, int(link.ID))
		}
		slices.Sort(ids)
		assert.Equal(t, ids, []int{2, 3, 4, 5, 6, 8})
	})
}

func testLinkDAO(t *testing.T, callback func(tx Transaction, dao *LinkDAO)) {
	testTransaction(t, func(tx Transaction) {
		callback(tx, NewLinkDAO(tx, &util.NullLogger))
	})
}
