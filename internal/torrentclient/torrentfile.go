package torrentclient

import (
	"fmt"

	"github.com/anacrolix/torrent/metainfo"
)

func torrentMetaInfoFromFile(path string) (*metainfo.MetaInfo, error) {
	mi, err := metainfo.LoadFromFile(path)
	if err != nil {
		return nil, fmt.Errorf("load .torrent file %s: %w", path, err)
	}
	return mi, nil
}
