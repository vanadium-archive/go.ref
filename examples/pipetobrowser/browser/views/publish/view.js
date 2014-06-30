import { View } from 'libs/mvc/view'

/*
 * View representing the state and interaction for publishing the p2b service.
 * @class
 * @extends {View}
 */
export class PublishView extends View {
  constructor(publishState) {
    var el = document.createElement('p2b-publish');
    el.publishState = publishState;
    super(el);
  }

/*
 * Event representing user's intention to publish the p2b service under the provided name
 * @event
 * @type {string} Requested name for service to be published under
 */
  onPublishAction(eventHandler) {
    this.element.addEventListener('publish', (e) => {
      eventHandler(e.detail.publishName);
    });
  }
}



/*veyron/examples/pipetobrowser: Supporting search (in message, file path, threadid),
filtering (log levels, date), sorting (all columns except message) for
veyron log viewer. Also adding ability to pause/resume the log viewer.

To support these features for veyron log viewer and other plugins,
a new reusable data grid component was created (libs/ui-components/data-grid)
data grid can host search, filters and supports sortable columns and custom cell renderer
example usage:

 <p2b-grid defaultSortKey="firstName"
  defaultSortAscending
  dataSource="{{ myContactsDataSource }}"
  summary="Displays your contacts in a tabular format">

  <!-- Search contacts-->
  <p2b-grid-search label="Search Contacts"></p2b-grid-search>

  <!-- Filter for circles -->
  <p2b-grid-filter-select multiple key="circle" label="Circles">
    <p2b-grid-filter-select-item checked label="Close Friends" value="close"></p2b-grid-filter-select-item>
    <p2b-grid-filter-select-item label="Colleagues" value="far"></p2b-grid-filter-select-item>
  </p2b-grid-filter-select>

  <!-- Toggle to allow filtering by online mode-->
  <p2b-grid-filter-toggle key="online" label="Show online only" checked></p2b-grid-filter-toggle>

  <!-- Columns, sorting and cell templates -->
  <p2b-grid-column sortable label="First Name" key="firstName" />
    <template>{{ item.firstName }}</template>
  </p2b-grid-column>

  <p2b-grid-column sortable label="Last Name" key="lastName" />
    <template>
      <span style="text-transform:uppercase;">
        {{ item.lastName }}
      </span>
    </template>
  </p2b-grid-column>

  <p2b-grid-column label="Circle" key="circle"/>
    <template>
      <img src="images\circls\{{ item.circle }}.jpg" alt="in {{ item.circle }} circle"><img>
    </template>
  </p2b-grid-column>

</p2b-grid>
Please see documentation in (libs/ui-components/data-grid/grid/component.html) for more details.

still TODO: UI cleanup
*/
