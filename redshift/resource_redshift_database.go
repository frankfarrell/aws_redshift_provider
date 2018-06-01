package redshift

//https://docs.aws.amazon.com/redshift/latest/dg/r_CREATE_DATABASE.html
//https://docs.aws.amazon.com/redshift/latest/dg/r_DROP_DATABASE.html
//https://docs.aws.amazon.com/redshift/latest/dg/r_ALTER_DATABASE.html

import (
	"database/sql"
	"github.com/hashicorp/terraform/helper/schema"
	"log"
	"time"
)

func redshiftDatabase() *schema.Resource {
	return &schema.Resource{
		Create: resourceRedshiftDatabaseCreate,
		Read:   resourceRedshiftDatabaseRead,
		Update: resourceRedshiftDatabaseUpdate,
		Delete: resourceRedshiftDatabaseDelete,
		Exists: resourceRedshiftDatabaseExists,
		Importer: &schema.ResourceImporter{
			State: resourceRedshiftDatabaseImport,
		},

		Schema: map[string]*schema.Schema{
			"database_name": { //This isn't immutable. The usesysid returned should be used as the id
				Type:     schema.TypeString,
				Required: true,
			},
			"owner": {
				Type:     schema.TypeInt,
				Required: true,
			},
			"connection_limit": { //Cluster limit is 500
				Type:     schema.TypeString,
				Optional: true,
				Default:  "UNLIMITED",
			},
		},
	}
}

func resourceRedshiftDatabaseExists(d *schema.ResourceData, meta interface{}) (b bool, e error) {
	// Exists - This is called to verify a resource still exists. It is called prior to Read,
	// and lowers the burden of Read to be able to assume the resource exists.
	client := meta.(*sql.DB)

	var name string

	err := client.QueryRow("SELECT datname FROM pg_database_info WHERE datid = $1", d.Id()).Scan(&name)
	if err != nil {
		return false, err
	}
	return true, nil
}

func resourceRedshiftDatabaseCreate(d *schema.ResourceData, meta interface{}) error {

	redshiftClient := meta.(*sql.DB)
	tx, txErr := redshiftClient.Begin()
	if txErr != nil {
		panic(txErr)
	}

	var createStatement string = "create database " + d.Get("database_name").(string)

	if v, ok := d.GetOk("owner"); ok {

		var usernames = GetUsersnamesForUsesysid(tx, []interface{}{v.(int)})
		createStatement += " OWNER " + usernames[0]
	}
	if v, ok := d.GetOk("connection_limit"); ok {
		createStatement += " CONNECTION LIMIT " + v.(string)
	}

	log.Print("Create database statement: " + createStatement)

	if _, err := tx.Exec(createStatement); err != nil {
		log.Fatal(err)
		return err
	}

	//The changes do not propagate instantly
	time.Sleep(5 * time.Second)

	var datid string
	err := tx.QueryRow("SELECT datid FROM pg_database_info WHERE datname = $1", d.Get("database_name").(string)).Scan(&datid)

	if err != nil {
		log.Fatal(err)
		return err
	}

	d.SetId(datid)

	readErr := readRedshiftDatabase(d, tx)

	if readErr == nil {
		tx.Commit()
		return nil
	} else {
		tx.Rollback()
		return readErr
	}
}

func resourceRedshiftDatabaseRead(d *schema.ResourceData, meta interface{}) error {

	redshiftClient := meta.(*sql.DB)
	tx, txErr := redshiftClient.Begin()
	if txErr != nil {
		panic(txErr)
	}

	err := readRedshiftDatabase(d, tx)

	if err == nil {
		tx.Commit()
		return nil
	} else {
		tx.Rollback()
		return err
	}
}

func readRedshiftDatabase(d *schema.ResourceData, tx *sql.Tx) error {
	var (
		databasename string
		owner        int
		connlimit    sql.NullString
	)

	err := tx.QueryRow("select datname, datdba, datconnlimit from pg_database_info where datid = $1", d.Id()).Scan(&databasename, &owner, &connlimit)

	if err != nil {
		log.Fatal(err)
		return err
	}

	d.Set("database_name", databasename)
	d.Set("owner", owner)

	if connlimit.Valid {
		d.Set("connection_limit", connlimit.String)
	} else {
		d.Set("connection_limit", nil)
	}

	return nil
}

func resourceRedshiftDatabaseUpdate(d *schema.ResourceData, meta interface{}) error {

	redshiftClient := meta.(*sql.DB)
	tx, txErr := redshiftClient.Begin()
	if txErr != nil {
		panic(txErr)
	}

	if d.HasChange("database_name") {

		oldName, newName := d.GetChange("database_name")
		alterDatabaseNameQuery := "ALTER DATABASE " + oldName.(string) + " rename to " + newName.(string)

		if _, err := tx.Exec(alterDatabaseNameQuery); err != nil {
			return err
		}
	}

	if d.HasChange("owner") {

		var username = GetUsersnamesForUsesysid(tx, []interface{}{d.Get("owner").(int)})

		if _, err := tx.Exec("ALTER DATABASE " + d.Get("database_name").(string) + " OWNER TO " + username[0]); err != nil {
			return err
		}
	}

	//TODO What if value is removed?
	if d.HasChange("connection_limit") {
		if _, err := tx.Exec("ALTER DATABASE " + d.Get("database_name").(string) + " CONNECTION LIMIT " + d.Get("connection_limit").(string)); err != nil {
			return err
		}
	}

	err := readRedshiftDatabase(d, tx)

	if err == nil {
		tx.Commit()
		return nil
	} else {
		tx.Rollback()
		return err
	}
}

func resourceRedshiftDatabaseDelete(d *schema.ResourceData, meta interface{}) error {

	client := meta.(*sql.DB)

	_, err := client.Exec("drop database " + d.Get("database_name").(string))

	if err != nil {
		log.Fatal(err)
		return err
	}

	return nil
}

func resourceRedshiftDatabaseImport(d *schema.ResourceData, meta interface{}) ([]*schema.ResourceData, error) {
	if err := resourceRedshiftDatabaseRead(d, meta); err != nil {
		return nil, err
	}
	return []*schema.ResourceData{d}, nil
}
