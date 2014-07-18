**TODO(aghassemi) Finish and clean up documentation, ensure it aligns with Design Doc on Google Drive.**

# Pipe to Browser
P2B allows one to pipe anything from shell console to the browser. Data being piped to the browser then is displayed in a graphical and formatted way by a "viewer" Viewers are pluggable pieces of code that know how to handle and display a stream of data.

For example one can do:

``
echo "Hi!" | p2b google/p2b/jane/console
``

or

``
cat cat.jpg | p2b -binary google/p2b/jane/image
``

where **google/p2b/jane** is the Object name where p2b service is running in the browser. The suffix **console** or **image** specifies what viewer should be used to display the data.

## Building and Running
To build
``
make
``
To run
``
make start #Starts a web server at 8080
``
and then navigate to http://localhost:8080

To stop
``
make stop
``

To clean
``
make clean
``

## Built-in Viewers
TODO

## How to Write Custom Viewer Plugins
TODO

## Concepts and Technical Design
### UI Framework
#### View
a View consists of:
A framework-specific wrapper that exposes the public API (ie. exposed properties and events ) of the View as a JavaScript module. This is what the application uses to interact with the view.
A Web Component that encapsulates all of the view including rendering, event binding, life-cycle management, attributes binding, etc… The Web Component exposes functionality of the view through properties and events as a DOM object.

Application never interacts with the Web Component part of the view directly. It only uses View’s wrapper API.

**Rationale:**
Web Component provides the functionality that traditional view frameworks ( e.g. Backbone , AngularJS views ) had to provide. Since a Web Component is a DOM element, it by default provides support for events and attributes. Web Component specification adds life-cycle management ( e.g. knowing when view is rendered, created or removed ), templating, data-binding and encapsulation in a single element. Bringing functionality on par with View frameworks out there.

The wrapper JS object around the Web Component adds a layer of indirection which exposes the public API in a cleaner way, and also hides the DOM object, which should be considered implementation details, from the rest of the application. This would allow us to switch the implementation of the View without changing the application.

#### Actions
Actions are cohesive pieces of functionality that can be triggered from any other action in a decoupled way by just using the action name. Actions glue views and application services and state.

Action names are a similar concepts to routes and simply provide an indirection through string names to register and call functions. Action handlers, which are functions tied to an action name, are a similar concept to controllers. Using actions to group and call larger, self-contained functionality like page transitions allows the framework to provide undo/back support by using history API or localStorage.

### Pipe To Browser

#### Pipe-Viewer
When a user sends a pipe request to p2b, the suffix of the Object name indicates what **viewer** should be used to display the data. For instance:

``
ls -l | p2b google/p2b/aghassemi/DataTable
``

indicates the DataTable viewer should be used to display the data whereas:

``
ls -l | p2b google/p2b/aghassemi/List
``

requests List viewer to be used.

A pipe-viewer is an interface defined by p2b as an abstract class. Any viewer that wants to plugin to the system has to inherit from that class. pipe-viewer contract demands that any implementation to have a unique name which is used to find the viewer plugin and a play function that will be called and supplied by a stream object.
play(stream)needs to return a View or promise of a View that will be appended to the UI by the p2b framework. If a promise, p2b UI will display a loading widget until promise is resolved.

pipe-viewer manager is responsible for finding and loading pipe-viewers on demand based on their name. This on-demand loading allows us to support arbitrary viewers even from external sources. For instance we should be able to support: (not implemented yet)

``
ls -l | p2b google/p2b/aghassemi/external/HypnoDataTable --viewer-source=http://www.p2bMarketPlace.com/plugins/HypnoDataTable.js
``