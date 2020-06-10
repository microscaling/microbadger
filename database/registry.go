package database

func (d *PgDB) GetRegistry(id string) (reg *Registry, err error) {
	// Need to initialize an empty Registry so that we get the table name correctly
	reg = &Registry{}
	err = d.db.Where("id = ?", id).First(reg).Error
	return reg, err
}

func (d *PgDB) PutRegistry(reg *Registry) error {
	err := d.db.FirstOrCreate(reg).Error
	return err
}
