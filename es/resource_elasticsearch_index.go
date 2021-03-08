package es

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/hashicorp/terraform-plugin-sdk/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/helper/validation"

	elastic7 "github.com/olivere/elastic/v7"
	elastic5 "gopkg.in/olivere/elastic.v5"
	elastic6 "gopkg.in/olivere/elastic.v6"
)

var (
	staticSettingsKeys = []string{
		"number_of_shards",
		"codec",
		"routing_partition_size",
		"load_fixed_bitset_filters_eagerly",
	}
	dynamicsSettingsKeys = []string{
		"number_of_replicas",
		"auto_expand_replicas",
		"refresh_interval",
		//"max_result_window"
		//"max_inner_result_window"
		//"max_rescore_window"
		//...
	}
	settingsKeys = append(staticSettingsKeys, dynamicsSettingsKeys...)
)

var (
	configSchema = map[string]*schema.Schema{
		"name": {
			Type:        schema.TypeString,
			Description: "Name of the index to create",
			ForceNew:    true,
			Required:    true,
		},
		"force_destroy": {
			Type:        schema.TypeBool,
			Description: "A boolean that indicates that the index should be deleted even if it contains documents.",
			Default:     false,
			Optional:    true,
		},
		// Static settings that can only be set on creation
		"number_of_shards": {
			Type:        schema.TypeString,
			Description: "Number of shards for the index. This can be set only on creation.",
			ForceNew:    true,
			Default:     "1",
			Optional:    true,
		},
		"routing_partition_size": {
			Type:        schema.TypeInt,
			Description: "The number of shards a custom routing value can go to. This can be set only on creation.",
			ForceNew:    true,
			Optional:    true,
		},
		"load_fixed_bitset_filters_eagerly": {
			Type:        schema.TypeBool,
			Description: "Indicates whether cached filters are pre-loaded for nested queries. This can be set only on creation.",
			ForceNew:    true,
			Optional:    true,
		},
		"codec": {
			Type:        schema.TypeString,
			Description: "The `default` value compresses stored data with LZ4 compression, but this can be set to `best_compression` which uses DEFLATE for a higher compression ratio. This can be set only on creation.",
			ForceNew:    true,
			Optional:    true,
		},
		// Dynamic settings that can be changed at runtime
		"number_of_replicas": {
			Type:        schema.TypeString,
			Description: "Number of shard replicas",
			Optional:    true,
		},
		"auto_expand_replicas": {
			Type:        schema.TypeString, // 0-5 OR 0-all
			Description: "Set the number of replicas to the node count in the cluster",
			Optional:    true,
		},
		"refresh_interval": {
			Type:        schema.TypeString,
			Description: "How often to perform a refresh operation, which makes recent changes to the index visible to search. Can be set to `-1` to disable refresh.",
			Optional:    true,
		},
		// Other attributes
		"mappings": {
			Type:         schema.TypeString,
			Description:  "A JSON string defining how documents in the index, and the fields they contain, are stored and indexed. To avoid the complexities of field mapping updates, updates of this field are not allowed via this provider. See the upstream [Elasticsearch docs](https://www.elastic.co/guide/en/elasticsearch/reference/6.8/indices-put-mapping.html#updating-field-mappings) for more details.",
			Optional:     true,
			ForceNew:     true,
			ValidateFunc: validation.StringIsJSON,
		},
		"aliases": {
			Type:        schema.TypeString,
			Description: "A JSON string describing a set of aliases. The index aliases API allows aliasing an index with a name, with all APIs automatically converting the alias name to the actual index name. An alias can also be mapped to more than one index, and when specifying it, the alias will automatically expand to the aliased indices.",
			Optional:    true,
			// In order to not handle the separate endpoint of alias updates, updates
			// are not allowed via this provider currently.
			ForceNew:     true,
			ValidateFunc: validation.StringIsJSON,
		},
		// Computed attributes
		"rollover_alias": {
			Type:     schema.TypeString,
			Optional: true,
			Computed: true,
		},
	}
)

func resourceElasticsearchIndex() *schema.Resource {
	return &schema.Resource{
		Description: "Provides an Elasticsearch index resource.",
		Create:      resourceElasticsearchIndexCreate,
		Read:        resourceElasticsearchIndexRead,
		Update:      resourceElasticsearchIndexUpdate,
		Delete:      resourceElasticsearchIndexDelete,
		Schema:      configSchema,
		Importer: &schema.ResourceImporter{
			State: schema.ImportStatePassthrough,
		},
	}
}

func resourceElasticsearchIndexCreate(d *schema.ResourceData, meta interface{}) error {
	var (
		name     = d.Get("name").(string)
		settings = settingsFromIndexResourceData(d)
		body     = make(map[string]interface{})
		ctx      = context.Background()
		err      error
	)
	if len(settings) > 0 {
		body["settings"] = settings
	}

	if aliasJSON, ok := d.GetOk("aliases"); ok {
		var aliases map[string]interface{}
		bytes := []byte(aliasJSON.(string))
		err = json.Unmarshal(bytes, &aliases)
		if err != nil {
			return fmt.Errorf("fail to unmarshal: %v", err)
		}
		body["aliases"] = aliases
	}

	if mappingsJSON, ok := d.GetOk("mappings"); ok {
		var mappings map[string]interface{}
		bytes := []byte(mappingsJSON.(string))
		err = json.Unmarshal(bytes, &mappings)
		if err != nil {
			return fmt.Errorf("fail to unmarshal: %v", err)
		}
		body["mappings"] = mappings
	}

	// if date math is used, we need to pass the resolved name along to the read
	// so we can pull the right result from the response
	var resolvedName string

	// Note: the CreateIndex call handles URL encoding under the hood to handle
	// non-URL friendly characters and functionality like date math
	esClient, err := getClient(meta.(*ProviderConf))
	if err != nil {
		return err
	}
	switch client := esClient.(type) {
	case *elastic7.Client:
		resp, requestErr := client.CreateIndex(name).BodyJson(body).Do(ctx)
		err = requestErr
		if err == nil {
			resolvedName = resp.Index
		}

	case *elastic6.Client:
		resp, requestErr := client.CreateIndex(name).BodyJson(body).Do(ctx)
		err = requestErr
		if err == nil {
			resolvedName = resp.Index
		}

	default:
		elastic5Client := client.(*elastic5.Client)
		resp, requestErr := elastic5Client.CreateIndex(name).BodyJson(body).Do(ctx)
		err = requestErr
		if err == nil {
			resolvedName = resp.Index
		}

	}

	if err == nil {
		// Let terraform know the resource was created
		d.SetId(resolvedName)
		return resourceElasticsearchIndexRead(d, meta)
	}
	return err
}

func settingsFromIndexResourceData(d *schema.ResourceData) map[string]interface{} {
	settings := make(map[string]interface{})
	for _, key := range settingsKeys {
		if raw, ok := d.GetOk(key); ok {
			settings[key] = raw
		}
	}
	return settings
}

func indexResourceDataFromSettings(settings map[string]interface{}, d *schema.ResourceData) {
	for _, key := range settingsKeys {
		rawValue, okRaw := settings[key]
		rawPrefixedValue, okPrefixed := settings["index."+key]
		var value interface{}
		if !okRaw && !okPrefixed {
			continue
		} else if okRaw {
			value = rawValue
		} else if okPrefixed {
			value = rawPrefixedValue
		}

		err := d.Set(key, value)
		if err != nil {
			log.Printf("[WARN] indexResourceDataFromSettings: %+v", err)
		}
	}
}

func resourceElasticsearchIndexDelete(d *schema.ResourceData, meta interface{}) error {
	var (
		name = d.Id()
		ctx  = context.Background()
		err  error
	)

	if alias, ok := d.GetOk("rollover_alias"); ok {
		name = getWriteIndexByAlias(alias.(string), d, meta)
	}

	// check to see if there are documents in the index
	allowed := allowIndexDestroy(name, d, meta)
	if !allowed {
		return fmt.Errorf("There are documents in the index (or the index could not be , set force_destroy to true to allow destroying.")
	}

	esClient, err := getClient(meta.(*ProviderConf))
	if err != nil {
		return err
	}
	switch client := esClient.(type) {
	case *elastic7.Client:
		_, err = client.DeleteIndex(name).Do(ctx)

	case *elastic6.Client:
		_, err = client.DeleteIndex(name).Do(ctx)

	default:
		elastic5Client := client.(*elastic5.Client)
		_, err = elastic5Client.DeleteIndex(name).Do(ctx)
	}

	return err
}

func allowIndexDestroy(indexName string, d *schema.ResourceData, meta interface{}) bool {
	force := d.Get("force_destroy").(bool)

	var (
		ctx   = context.Background()
		count int64
		err   error
	)
	esClient, err := getClient(meta.(*ProviderConf))
	if err != nil {
		return false
	}
	switch client := esClient.(type) {
	case *elastic7.Client:
		count, err = client.Count(indexName).Do(ctx)

	case *elastic6.Client:
		count, err = client.Count(indexName).Do(ctx)

	default:
		elastic5Client := client.(*elastic5.Client)
		count, err = elastic5Client.Count(indexName).Do(ctx)
	}

	if err != nil {
		log.Printf("[INFO] allowIndexDestroy: %+v", err)
		return false
	}

	if count > 0 && !force {
		return false
	}
	return true
}

func resourceElasticsearchIndexUpdate(d *schema.ResourceData, meta interface{}) error {
	settings := make(map[string]interface{})
	for _, key := range settingsKeys {
		if d.HasChange(key) {
			settings[key] = d.Get(key)
		}
	}

	// if we're not changing any settings, no-op this function
	if len(settings) == 0 {
		return resourceElasticsearchIndexRead(d, meta)
	}

	body := map[string]interface{}{
		"settings": settings,
	}

	var (
		name = d.Id()
		ctx  = context.Background()
		err  error
	)

	if alias, ok := d.GetOk("rollover_alias"); ok {
		name = getWriteIndexByAlias(alias.(string), d, meta)
	}

	esClient, err := getClient(meta.(*ProviderConf))
	if err != nil {
		return err
	}
	switch client := esClient.(type) {
	case *elastic7.Client:
		_, err = client.IndexPutSettings(name).BodyJson(body).Do(ctx)

	case *elastic6.Client:
		_, err = client.IndexPutSettings(name).BodyJson(body).Do(ctx)

	default:
		elastic5Client := client.(*elastic5.Client)
		_, err = elastic5Client.IndexPutSettings(name).BodyJson(body).Do(ctx)
	}

	if err == nil {
		return resourceElasticsearchIndexRead(d, meta.(*ProviderConf))
	}
	return err
}

func getWriteIndexByAlias(alias string, d *schema.ResourceData, meta interface{}) string {
	var (
		index   = d.Id()
		ctx     = context.Background()
		columns = []string{"index", "is_write_index"}
	)

	esClient, err := getClient(meta.(*ProviderConf))
	if err != nil {
		log.Printf("[INFO] getWriteIndexByAlias: %+v", err)
		return index
	}
	switch client := esClient.(type) {
	case *elastic7.Client:
		r, err := client.CatAliases().Alias(alias).Columns(columns...).Do(ctx)
		if err != nil {
			log.Printf("[INFO] getWriteIndexByAlias: %+v", err)
			return index
		}
		for _, column := range r {
			if column.IsWriteIndex == "true" {
				return column.Index
			}
		}

	case *elastic6.Client:
		r, err := client.CatAliases().Alias(alias).Columns(columns...).Do(ctx)
		if err != nil {
			log.Printf("[INFO] getWriteIndexByAlias: %+v", err)
			return index
		}
		for _, column := range r {
			if column.IsWriteIndex == "true" {
				return column.Index
			}
		}

	default:
		elastic5Client := client.(*elastic5.Client)
		r, err := elastic5Client.CatAliases().Alias(alias).Columns(columns...).Do(ctx)
		if err != nil {
			log.Printf("[INFO] getWriteIndexByAlias: %+v", err)
			return index
		}
		for _, column := range r {
			if column.IsWriteIndex == "true" {
				return column.Index
			}
		}
	}

	return index
}

func resourceElasticsearchIndexRead(d *schema.ResourceData, meta interface{}) error {
	var (
		index    = d.Id()
		ctx      = context.Background()
		settings map[string]interface{}
	)

	if alias, ok := d.GetOk("rollover_alias"); ok {
		index = getWriteIndexByAlias(alias.(string), d, meta)
	}

	// The logic is repeated strictly because of the types
	esClient, err := getClient(meta.(*ProviderConf))
	if err != nil {
		return err
	}
	switch client := esClient.(type) {
	case *elastic7.Client:
		r, err := client.IndexGetSettings(index).FlatSettings(true).Do(ctx)
		if err != nil {
			if elastic7.IsNotFound(err) {
				log.Printf("[WARN] Index (%s) not found, removing from state", index)
				d.SetId("")
				return nil
			}

			return err
		}

		if resp, ok := r[index]; ok {
			settings = resp.Settings
		}
	case *elastic6.Client:
		r, err := client.IndexGetSettings(index).FlatSettings(true).Do(ctx)
		if err != nil {
			if elastic6.IsNotFound(err) {
				log.Printf("[WARN] Index (%s) not found, removing from state", index)
				d.SetId("")
				return nil
			}
			return err
		}

		if resp, ok := r[index]; ok {
			settings = resp.Settings
		}
	default:
		elastic5Client := client.(*elastic5.Client)
		r, err := elastic5Client.IndexGetSettings(index).FlatSettings(true).Do(ctx)
		if err != nil {
			if elastic5.IsNotFound(err) {
				log.Printf("[WARN] Index (%s) not found, removing from state", index)
				d.SetId("")
				return nil
			}
			return err
		}

		if resp, ok := r[index]; ok {
			settings = resp.Settings
		}
	}

	// Don't override name otherwise it will force a replacement
	if _, ok := d.GetOk("name"); !ok {
		name := index
		if providedName, ok := settings["index.provided_name"].(string); ok {
			name = providedName
		}
		err := d.Set("name", name)
		if err != nil {
			return err
		}
	}

	// If index is managed by ILM or ISM set rollover_alias
	if alias, ok := settings["index.lifecycle.rollover_alias"].(string); ok {
		err := d.Set("rollover_alias", alias)
		if err != nil {
			return err
		}
	} else if alias, ok := settings["opendistro.index_state_management.rollover_alias"].(string); ok {
		err := d.Set("rollover_alias", alias)
		if err != nil {
			return err
		}
	}

	indexResourceDataFromSettings(settings, d)

	return nil
}
