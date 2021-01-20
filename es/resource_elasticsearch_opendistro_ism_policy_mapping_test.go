package es

import (
	"fmt"
	"testing"

	elastic7 "github.com/olivere/elastic/v7"
	elastic5 "gopkg.in/olivere/elastic.v5"
	elastic6 "gopkg.in/olivere/elastic.v6"

	"github.com/hashicorp/terraform-plugin-sdk/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/terraform"
)

func TestAccElasticsearchOpenDistroISMPolicyMapping(t *testing.T) {
	provider := Provider().(*schema.Provider)
	err := provider.Configure(&terraform.ResourceConfig{})
	if err != nil {
		t.Skipf("err: %s", err)
	}
	meta := provider.Meta()
	esClient, err := getClient(meta.(*ProviderConf))
	if err != nil {
		t.Skipf("err: %s", err)
	}
	var allowed bool

	switch esClient.(type) {
	case *elastic6.Client:
		allowed = false
	case *elastic5.Client:
		allowed = false
	default:
		allowed = true
	}

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
			if !allowed {
				t.Skip("OpenDistroISMPolicies only supported on ES 7.")
			}
		},
		Providers:    testAccOpendistroProviders,
		CheckDestroy: testCheckElasticsearchOpenDistroISMPolicyMappingDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccElasticsearchOpenDistroISMPolicyMapping,
				Check: resource.ComposeTestCheckFunc(
					testCheckElasticsearchOpenDistroISMPolicyMappingExists("elasticsearch_opendistro_ism_policy_mapping.test_mapping"),
					resource.TestCheckResourceAttr(
						"elasticsearch_opendistro_ism_policy_mapping.test_mapping",
						"policy_id",
						"test_policy",
					),
				),
			},
		},
	})
}

func testCheckElasticsearchOpenDistroISMPolicyMappingExists(name string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[name]
		if !ok {
			return fmt.Errorf("Not found: %s", name)
		}
		if rs.Primary.ID == "" {
			return fmt.Errorf("No policy ID is set")
		}

		meta := testAccOpendistroProvider.Meta()

		var err error
		esClient, err := getClient(meta.(*ProviderConf))
		if err != nil {
			return err
		}
		switch esClient.(type) {
		case *elastic7.Client:
			_, err = resourceElasticsearchGetOpendistroPolicyMapping(rs.Primary.ID, meta.(*ProviderConf))
		default:
		}

		if err != nil {
			return err
		}

		return nil
	}
}

func testCheckElasticsearchOpenDistroISMPolicyMappingDestroy(s *terraform.State) error {
	for _, rs := range s.RootModule().Resources {
		if rs.Type != "elasticsearch_opendistro_ism_policy_mapping" {
			continue
		}

		meta := testAccOpendistroProvider.Meta()

		var err error
		esClient, err := getClient(meta.(*ProviderConf))
		if err != nil {
			return err
		}
		switch esClient.(type) {
		case *elastic7.Client:
			_, err = resourceElasticsearchGetOpendistroPolicyMapping(rs.Primary.ID, meta.(*ProviderConf))
		default:
		}

		if err != nil {
			return nil // should be not found error
		}

		return fmt.Errorf("OpenDistroISMPolicyMapping %q still exists", rs.Primary.ID)
	}

	return nil
}

var testAccElasticsearchOpenDistroISMPolicyMapping = `
resource "elasticsearch_opendistro_ism_policy" "test_policy" {
	policy_id = "test_policy"
	body      = <<EOF
 {
	"policy": {
	  "description": "ingesting logs",
	  "schema_version": 1,
	  "default_state": "ingest",
	  "states": [
		{
		  "name": "ingest",
		  "actions": [
			{
			  "rollover": {
				"min_doc_count": 5
			  }
			}
		  ],
		  "transitions": [
			{
			  "state_name": "search"
			}
		  ]
		},
		{
		  "name": "search",
		  "actions": [],
		  "transitions": [
			{
			  "state_name": "delete",
			  "conditions": {
				"min_index_age": "5m"
			  }
			}
		  ]
		},
		{
		  "name": "delete",
		  "actions": [
			{
			  "delete": {}
			}
		  ],
		  "transitions": []
		}
	  ]
	}
 }
 EOF
}

resource "elasticsearch_opendistro_ism_policy_mapping" "test_mapping" {
	policy_id = "${elasticsearch_opendistro_ism_policy.test_policy.id}"
	indexes = "filebeat*"
}
`
