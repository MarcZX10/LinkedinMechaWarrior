package api

// The GraphQL queries lmw uses, with the hashes seen working in public
// clients of the web app, most recently verified first. `lmw har learn`
// adds the hashes LinkedIn's website uses today.
var (
	profileUpdatesQuery = Query{
		Name: "voyagerFeedDashProfileUpdates", Vars: []string{"count", "start", "profileUrn"}, Optional: []string{"paginationToken"},
		Hashes: []string{"20c70fe0314184158516a7ec004c0408", "be2623b61657a92a693eb8d9c67aeec0"},
	}
	updateQuery = Query{
		Name: "voyagerFeedDashUpdates", Vars: []string{"updateUrn"},
		Hashes: []string{"c2a318e55b634e20689c80e3dd11952e"},
	}
	commentsQuery = Query{
		Name: "voyagerSocialDashComments", Vars: []string{"count", "numReplies", "socialDetailUrn", "sortOrder", "start"},
		Optional: []string{"paginationToken"},
		Hashes:   []string{"afec6d88d7810d45548797a8dac4fb87"},
	}
	reactionsQuery = Query{
		Name: "voyagerSocialDashReactions", Vars: []string{"count", "start", "threadUrn"}, Optional: []string{"reactionType"},
		Hashes: []string{"41ebf31a9f4c4a84e35a49d5abc9010b", "759f60600a6108d4110bc8b8f6ecd507"},
	}
	reactQuery = Query{
		Name: "voyagerSocialDashReactions", Vars: []string{"entity", "threadUrn"},
		Hashes: []string{"b731222600772fd42464c0fe19bd722b"},
	}
	unreactQuery = Query{
		Name: "voyagerSocialDashReactions", Vars: []string{"threadUrn"}, Optional: []string{"reactionType"},
		Hashes: []string{"f68b48ae5bc0085d7a45c7003b772a39"},
	}
	searchQuery = Query{
		Name: "voyagerSearchDashClusters", Vars: []string{"start", "origin", "query"}, Optional: []string{"count"},
		Hashes: []string{"b0928897b71bd00a5a7291755dcd64f0", "e438ab99259203e9c1cd3f358e217282", "9a78a658089851e133c5b7bb2b4baee5"},
	}
	conversationsQuery = Query{
		Name: "messengerConversations", Messaging: true, Vars: []string{"query", "count", "mailboxUrn"}, Optional: []string{"nextCursor"},
		Hashes: []string{"9501074288a12f3ae9e3c7ea243bccbf"},
	}
	messagesQuery = Query{
		Name: "messengerMessages", Messaging: true, Vars: []string{"conversationUrn"},
		Hashes: []string{"5846eeb71c981f11e0134cb6626cc314"},
	}
)

// Queries lists the GraphQL queries lmw uses.
func Queries() []Query {
	return []Query{profileUpdatesQuery, updateQuery, commentsQuery, reactionsQuery, reactQuery, unreactQuery,
		searchQuery, peopleTypeaheadQuery, savedItemsQuery, conversationsQuery, messagesQuery, pollQuery, urlPreviewQuery}
}
