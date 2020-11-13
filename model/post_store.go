package model

import (
	"database/sql"
	"time"
)

type postStore struct {
	conn *sql.DB
}

var GlobalPostStore PostStore

const selectFromPost = `select * from post `
const paginate = ` order by p.created_at, p.id limit ? offset ?; `
const selectByJoinPost = `
	select p.*, u.username, u.email, u.photo_id, c.name, c.name_code, c.description from post as p 
	left join user as u 
		on p.user_id = u.id 
	left join category as c 
		on p.category_id = c.id `
const selectPostCategory = `
	select p.*, u.username, u.email, u.photo_id,
			c.name, c.name_code, c.description
	from (
		select * from post_categories as pc
		where pc.category_id = ?
	) as pc,
	post as p,
	user as u,
	category as c
	where p.id = pc.post_id
		and p.user_id = u.id
		and pc.category_id = c.id

`

func InitGlobalPostStore() {
	GlobalPostStore = DB.postStore
}

func (s *postStore) New(post *Post) (int64, error) {
	stmt, err := s.conn.Prepare("insert into post(user_id, title, text, created_at, photo_id, category) values(?, ?, ?, ?, ?, ?)")
	if err != nil {
		return 0, err
	}

	res, err := stmt.Exec(post.User.ID, post.Title, post.Text, post.CreatedAt, post.PhotoID, post.Category.ID)
	if err != nil {
		return 0, err
	}

	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}

	return id, err
}

func (s *postStore) Get(id int64) (*Post, error) {
	row := s.conn.QueryRow(selectByJoinPost+` where p.id=$1`, id)
	return s.scanPost(row)
}

func (s *postStore) GetLatest(offset, limit int) ([]*Post, error) {
	query := selectByJoinPost
	query += paginate
	rows, err := s.conn.Query(query, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var posts []*Post
	for rows.Next() {
		user, err := s.scanPost(rows)
		if err != nil {
			return nil, err
		}
		posts = append(posts, user)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return posts, nil
}

func (s *postStore) GetUserPosts(userID string, offset, limit int) ([]*Post, error) {
	query := selectByJoinPost
	query += ` where p.user_id = $3`
	query += paginate
	rows, err := s.conn.Query(query, offset, limit, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var posts []*Post
	for rows.Next() {
		user, err := s.scanPost(rows)
		if err != nil {
			return nil, err
		}
		posts = append(posts, user)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return posts, nil
}

func (s *postStore) GetByCategory(offset, limit, category int) ([]*Post, error) {
	query := selectPostCategory
	query += paginate
	rows, err := s.conn.Query(query, category, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var posts []*Post
	for rows.Next() {
		user, err := s.scanPost(rows)
		if err != nil {
			return nil, err
		}
		posts = append(posts, user)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return posts, nil
}

func (s *postStore) Delete(id int64) error {
	stmt, err := s.conn.Prepare("delete from post where id=?")
	if err != nil {
		return err
	}

	_, err = stmt.Exec(id)
	if err != nil {
		return err
	}
	return nil
}

func (s *postStore) get(id int64) (*Post, error) {
	row := s.conn.QueryRow(`
		select like_count, dislike_count, comment_count from post where id=$1
	`, id)
	u := new(Post)
	err := row.Scan(
		&u.LikeCount,
		&u.DislikeCount,
		&u.CommentCount,
	)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return u, nil
}

func (s *postStore) IncrementReaction(id int64, reaction string) error {
	stmt, err := s.conn.Prepare("update post set " + reaction + "=(select " + reaction + " from post where id=?)+1 where id=?")
	if err != nil {
		return err
	}
	res, err := stmt.Exec(id, id)
	if err != nil {
		return err
	}

	affect, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affect == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *postStore) DecrementReaction(id int64, reaction string) error {
	stmt, err := s.conn.Prepare(
		"update post set " + reaction + "=(select " + reaction +
			" from post where id=?)-1 where id=? and like_count>0")
	if err != nil {
		return err
	}
	res, err := stmt.Exec(id, id)
	if err != nil {
		return err
	}

	affect, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affect == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *postStore) IncrementCommentCount(id int64) error {
	stmt, err := s.conn.Prepare("update post set comment_count=(select comment_count from post where id=?)+1 where id=?")
	if err != nil {
		return err
	}
	res, err := stmt.Exec(id, id)
	if err != nil {
		return err
	}

	affect, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affect == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *postStore) HasReacted(postID int, userID string, reaction int) (bool, error) {
	stmt, err := s.conn.Prepare(
		`delete from user_reactions where post_id=$1 and user_id=$2 and reaction=$3`)
	if err != nil {
		return false, err
	}

	res, err := stmt.Exec(postID, userID, reaction)
	if err != nil {
		return false, err
	}

	affect, err := res.RowsAffected()
	if err != nil {
		return false, err
	}

	return 0 != affect, nil
}

func (s *postStore) NewUserReaction(postID int, userID string, reaction int) error {
	stmt, err := s.conn.Prepare("insert into user_reactions values(?, ?, ?)")
	if err != nil {
		return err
	}

	res, err := stmt.Exec(postID, userID, reaction)
	if err != nil {
		return err
	}

	id, err := res.LastInsertId()
	if err != nil || id == 0 {
		return ErrNotInserted
	}

	return nil
}

func (s *postStore) scanPost(scanner scanner) (*Post, error) {
	u := new(Post)
	user := new(User)
	cat := new(Category)

	var expiry string
	err := scanner.Scan(
		&u.ID,
		&u.UserID,
		&u.Title,
		&u.Text,
		&expiry,
		&u.LikeCount,
		&u.DislikeCount,
		&u.CommentCount,
		&u.PhotoID,
		&cat.ID,
		&user.Username,
		&user.Email,
		&user.PhotoID,
		&cat.Name,
		&cat.NameCode,
		&cat.Description,
	)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	u.User = user
	u.Category = cat

	parsed, err := time.Parse("2006-01-02 03:04:05.999999999-07:00", expiry)
	if err == nil {
		u.CreatedAt = parsed
	}
	return u, nil
}

func (s *postStore) scanPostCategory(scanner scanner) (*Post, error) {
	u := new(Post)
	cat := new(Category)

	var expiry string
	err := scanner.Scan(
		&u.ID,
		&u.UserID,
		&u.Title,
		&u.Text,
		&expiry,
		&u.LikeCount,
		&u.DislikeCount,
		&u.CommentCount,
		&u.PhotoID,
		&cat.ID,
	)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	u.Category = cat

	parsed, err := time.Parse("2006-01-02 03:04:05.999999999-07:00", expiry)
	if err == nil {
		u.CreatedAt = parsed
	}
	return u, nil
}